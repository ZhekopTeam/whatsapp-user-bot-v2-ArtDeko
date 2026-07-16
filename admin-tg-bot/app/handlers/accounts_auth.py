from contextlib import suppress

from aiogram import F, Router
from aiogram.exceptions import TelegramBadRequest
from aiogram.fsm.context import FSMContext
from aiogram.types import (
    BufferedInputFile,
    CallbackQuery,
    InputMediaPhoto,
    Message,
)

from app.keyboards import (
    account_proxy_pick_kb,
    accounts_list_kb,
    auth_cancel_kb,
)
from utils.access import is_admin
from utils.database import Account, AccountRepository, ProxyRepository
from utils.FSM import AddAccount
from utils.logger import logger
from utils.session_repo import get_session_accounts
from utils.sheets_sync import sync_accounts
from utils.wa_auth import (
    WhatsAppAuth,
    build_proxy_url,
    is_valid_phone,
    normalize_phone,
    render_qr_png,
)

router_auth = Router(name="auth")

_QR_CAPTION = (
    "📲 Откройте WhatsApp на телефоне с номером <b>{phone}</b>\n"
    "Настройки → Связанные устройства → Привязка устройства\n"
    "и отсканируйте этот QR-код."
)


async def _persist_account(
    phone: str,
    jid: str | None,
    admin_tg_id: int,
    proxy_id: str | None = None,
) -> None:
    repo = AccountRepository()
    existing = await repo.get_by_phone(phone)
    if existing:
        await repo.reactivate(phone, jid, admin_tg_id, proxy_id=proxy_id)
    else:
        await repo.add(
            Account(phone=phone, jid=jid, admin_tg_id=admin_tg_id,
                    status="active", proxy_id=proxy_id)
        )


@router_auth.callback_query(F.data == "add_account")
async def cb_add_account(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await state.set_state(AddAccount.waiting_phone)
    await callback.message.edit_text(
        "Введите номер телефона аккаунта в формате <b>+79991234567</b>.\n"
        "Можно также ввести номер, начинающийся с 8 — бот примет его автоматически.",
        reply_markup=auth_cancel_kb()
    )
    await callback.answer()


@router_auth.message(AddAccount.waiting_phone)
async def handle_phone(message: Message, state: FSMContext) -> None:
    raw = message.text.strip()
    if not is_valid_phone(raw):
        await message.answer("⚠️ Некорректный номер. Пример: <b>+79991234567</b>")
        return

    digits = normalize_phone(raw)
    if len(digits) == 11 and digits.startswith("8"):
        digits = "7" + digits[1:]

    phone = "+" + digits
    repo = AccountRepository()
    existing = await repo.get_by_phone(phone)
    if existing and existing.status == "active":
        await state.clear()
        accounts = await get_session_accounts()
        await message.answer(
            f"Аккаунт {phone} уже авторизован.",
            reply_markup=accounts_list_kb(accounts),
        )
        return

    free_proxies = await ProxyRepository().list_free()
    rows = [
        (p.id, p.name, p.proxy_type, p.host, p.port) for p in free_proxies
    ]
    await state.update_data(phone=phone)
    await state.set_state(AddAccount.choosing_proxy)
    await message.answer(
        f"📱 Номер: <b>{phone}</b>\n\n"
        "🌐 <b>Выберите прокси для этого аккаунта.</b>\n\n"
        "Прокси применяется <b>сразу при авторизации</b> и потом в работе — "
        "IP не меняется, это снижает риск слёта сессии.\n"
        "Один прокси — один аккаунт.\n\n"
        + (
            "Свободных прокси нет — можно продолжить без прокси "
            "(не рекомендуется)."
            if not rows
            else "Доступные прокси:"
        ),
        reply_markup=account_proxy_pick_kb(rows),
    )


@router_auth.callback_query(
    AddAccount.choosing_proxy, F.data.startswith("acc_proxy:")
)
async def cb_pick_account_proxy(
    callback: CallbackQuery, state: FSMContext, wa_auth: WhatsAppAuth
) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return

    proxy_choice = callback.data.split(":", 1)[1]
    data = await state.get_data()
    phone = data.get("phone")
    if not phone:
        await state.clear()
        await callback.answer("Сессия истекла, начните заново", show_alert=True)
        return

    proxy_id: str | None = None
    proxy_url: str | None = None
    if proxy_choice != "none":
        proxy = await ProxyRepository().get_by_id(proxy_choice)
        if not proxy:
            await callback.answer("Прокси не найден", show_alert=True)
            return
        # Guard against double-assignment (proxy is one-per-account).
        owner = await ProxyRepository().get_account_for_proxy(proxy.id)
        if owner is not None:
            await callback.answer(
                "Этот прокси уже привязан к другому аккаунту", show_alert=True
            )
            return
        proxy_id = proxy.id
        proxy_url = build_proxy_url(proxy)

    await callback.answer()
    with suppress(TelegramBadRequest):
        await callback.message.delete()

    await _run_account_auth(
        callback.message, state, wa_auth,
        phone=phone, proxy_id=proxy_id, proxy_url=proxy_url,
        admin_tg_id=callback.from_user.id,
    )


async def _run_account_auth(
    message: Message,
    state: FSMContext,
    wa_auth: WhatsAppAuth,
    *,
    phone: str,
    proxy_id: str | None,
    proxy_url: str | None,
    admin_tg_id: int,
) -> None:
    await state.set_state(AddAccount.waiting_qr)
    status_msg = await message.answer(
        "⏳ Запускаю авторизацию WhatsApp…", reply_markup=auth_cancel_kb()
    )
    photo_msg: Message | None = None
    terminal = False

    try:
        async for event in wa_auth.stream(admin_tg_id, phone, proxy_url):
            etype = event.get("event")

            if etype == "qr":
                png = render_qr_png(event["code"])
                media = BufferedInputFile(png, filename="wa_qr.png")
                caption = _QR_CAPTION.format(phone=phone)
                if photo_msg is None:
                    photo_msg = await message.answer_photo(
                        media, caption=caption, reply_markup=auth_cancel_kb()
                    )
                    with suppress(TelegramBadRequest):
                        await status_msg.delete()
                else:
                    with suppress(TelegramBadRequest):
                        await photo_msg.edit_media(
                            InputMediaPhoto(media=media, caption=caption),
                            reply_markup=auth_cancel_kb(),
                        )

            elif etype in ("success", "already_authorized"):
                terminal = True
                await _persist_account(
                    phone, event.get("jid"), admin_tg_id, proxy_id=proxy_id
                )
                logger.info(f"WhatsApp account {phone} authorized")
                await _show_result(message, photo_msg, status_msg, "✅ Аккаунт успешно добавлен!")
                await sync_accounts()
                break

            elif etype in ("timeout", "error"):
                terminal = True
                await _show_result(
                    message, photo_msg, status_msg, f"❌ Не удалось авторизовать"
                )
                break
    except (PermissionError, RuntimeError) as e:
        terminal = True
        await _show_result(message, photo_msg, status_msg, f"❌ Не удалось авторизовать")
        logger.info(f"WhatsApp auth error: {e}")
    except Exception as e:
        terminal = True
        logger.exception(f"WhatsApp auth error: {e}")
        await _show_result(message, photo_msg, status_msg, f"❌ Не удалось авторизовать")
    finally:
        await state.clear()

    if not terminal:
        logger.info("WhatsApp auth stream finished without terminal event")


@router_auth.callback_query(F.data == "auth:cancel")
async def cb_cancel_auth(
    callback: CallbackQuery, state: FSMContext, wa_auth: WhatsAppAuth
) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await wa_auth.cancel()
    await state.clear()
    with suppress(TelegramBadRequest):
        await callback.message.delete()
    accounts = await get_session_accounts()
    await callback.message.answer(
        "❌ Авторизация отменена.",
        reply_markup=accounts_list_kb(accounts),
    )
    await callback.answer()


async def _show_result(
    message: Message,
    photo_msg: Message | None,
    status_msg: Message | None,
    text: str,
) -> None:
    for msg in (photo_msg, status_msg):
        if msg is not None:
            with suppress(TelegramBadRequest):
                await msg.delete()
    accounts = await get_session_accounts()
    await message.answer(text, reply_markup=accounts_list_kb(accounts))

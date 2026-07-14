import re
from uuid import uuid4

from aiogram import F, Router
from aiogram.fsm.context import FSMContext
from aiogram.types import CallbackQuery, Message

from app.keyboards import (
    proxy_assign_list_kb,
    proxy_cancel_kb,
    proxy_detail_kb,
    proxy_list_kb,
)
from utils.access import is_admin
from utils.FSM import AddProxy
from utils.database import Proxy, ProxyRepository
from utils.logger import logger

router_proxy = Router(name="proxy")


async def _proxy_rows() -> list[tuple[str, str, str, str, int, int, bool]]:
    repo = ProxyRepository()
    proxies = await repo.get_all()
    rows = []
    for p in proxies:
        accounts = await repo.get_accounts_for_proxy(p.id)
        usage_count = len(accounts)
        is_busy = usage_count >= 6
        rows.append((p.id, p.name, p.proxy_type, p.host, p.port, usage_count, is_busy))
    return rows


def parse_proxy(text: str) -> dict | None:
    text = text.strip()
    ptype = "socks5"

    lines = [l.strip() for l in text.splitlines() if l.strip()]
    for line in lines:
        upper = line.upper()
        if upper.startswith("SOCKS5 ") or upper.startswith("SOCKS4 ") or upper.startswith("HTTP "):
            text = line
            break

    for scheme in ("SOCKS5", "SOCKS4", "HTTP", "HTTPS"):
        if text.upper().startswith(scheme + " "):
            ptype = scheme.lower()
            text = text[len(scheme):].strip()
            break

    url_match = re.match(
        r"^(socks5|socks4|https?):\/\/(?:([^:@]+):([^@]*)@)?([^:]+):(\d+)$",
        text, re.IGNORECASE
    )
    if url_match:
        return {
            "ptype": url_match.group(1).lower(),
            "username": url_match.group(2),
            "password": url_match.group(3),
            "host": url_match.group(4),
            "port": int(url_match.group(5)),
        }

    colon4 = re.match(r"^([^:]+):(\d+):([^:]+):([^:]+)$", text)
    if colon4:
        return {
            "ptype": ptype,
            "username": colon4.group(3),
            "password": colon4.group(4),
            "host": colon4.group(1),
            "port": int(colon4.group(2)),
        }

    colon2 = re.match(r"^([^:]+):(\d+)$", text)
    if colon2:
        return {
            "ptype": ptype,
            "username": None,
            "password": None,
            "host": colon2.group(1),
            "port": int(colon2.group(2)),
        }

    return None


# ── Proxy menu ─────────────────────────────────────────────────────────────────

@router_proxy.callback_query(F.data == "menu:proxy")
async def cb_proxy_menu(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await state.clear()
    rows = await _proxy_rows()
    text = (
        f"🌐 <b>Прокси</b> ({len(rows)}):" if rows
        else "🌐 Прокси не добавлены."
    )
    await callback.message.edit_text(text, reply_markup=proxy_list_kb(rows))
    await callback.answer()


@router_proxy.callback_query(F.data.startswith("proxy_detail:"))
async def cb_proxy_detail(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    proxy_id = callback.data.split(":", 1)[1]
    repo = ProxyRepository()
    proxy = await repo.get_by_id(proxy_id)
    if not proxy:
        await callback.answer("Прокси не найден", show_alert=True)
        return

    accounts = await repo.get_accounts_for_proxy(proxy_id)
    usage_count = len(accounts)
    is_busy = usage_count >= 6

    if usage_count > 0:
        from utils.session_repo import mask_phone
        accounts_list = "\n".join([f"• <b>{mask_phone(acc.phone)}</b>" for acc in accounts])
        status_str = f"Используется аккаунтами ({usage_count}/6):\n{accounts_list}"
    else:
        status_str = "🟢 Свободен (0/6)"

    auth = f"{proxy.username}:***@" if proxy.username else ""
    text = (
        f"🌐 <b>{proxy.name}</b>\n\n"
        f"Тип: <code>{proxy.proxy_type}</code>\n"
        f"Адрес: <code>{auth}{proxy.host}:{proxy.port}</code>\n\n"
        f"Статус:\n{status_str}"
    )
    await callback.message.edit_text(text, reply_markup=proxy_detail_kb(proxy_id))
    await callback.answer()


@router_proxy.callback_query(F.data.startswith("proxy_del:"))
async def cb_proxy_delete(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    proxy_id = callback.data.split(":", 1)[1]
    await ProxyRepository().delete(proxy_id)
    await callback.answer("🗑 Удалено")
    rows = await _proxy_rows()
    text = (
        f"🌐 <b>Прокси</b> ({len(rows)}):" if rows
        else "🌐 Прокси не добавлены."
    )
    await callback.message.edit_text(text, reply_markup=proxy_list_kb(rows))


# ── Add proxy FSM ──────────────────────────────────────────────────────────────

@router_proxy.callback_query(F.data == "proxy_add")
async def cb_proxy_add(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await state.set_state(AddProxy.waiting_proxy_input)
    sent_msg = await callback.message.edit_text(
        "Введите строку подключения прокси в одном из форматов:\n\n"
        "<code>socks5://user:pass@host:port</code>\n"
        "<code>SOCKS5 host:port:user:pass</code>\n"
        "<code>host:port:user:pass</code>",
        reply_markup=proxy_cancel_kb(),
    )
    await state.update_data(last_msg_id=sent_msg.message_id)
    await callback.answer()


@router_proxy.message(AddProxy.waiting_proxy_input)
async def msg_proxy_input(message: Message, state: FSMContext) -> None:
    if not is_admin(message.from_user.id):
        return

    data = await state.get_data()
    last_msg_id = data.get("last_msg_id")
    if last_msg_id:
        try:
            await message.bot.edit_message_reply_markup(
                chat_id=message.chat.id,
                message_id=last_msg_id,
                reply_markup=None
            )
        except Exception:
            pass

    parsed = parse_proxy(message.text.strip())
    if not parsed:
        sent = await message.answer(
            "❌ Неверный формат.\n"
            "Вы можете ввести в одном из следующих форматов:\n"
            "• <code>socks5://user:pass@host:port</code>\n"
            "• <code>SOCKS5 host:port:user:pass</code>\n"
            "• <code>host:port:user:pass</code>",
            reply_markup=proxy_cancel_kb(),
        )
        await state.update_data(last_msg_id=sent.message_id)
        return
    await state.update_data(parsed=parsed)
    await state.set_state(AddProxy.waiting_proxy_name)
    sent = await message.answer(
        "Введите понятное <b>название</b> для этого прокси\n"
        "(например, <i>Proxy-Germany-1</i>):",
        reply_markup=proxy_cancel_kb(),
    )
    await state.update_data(last_msg_id=sent.message_id)


@router_proxy.message(AddProxy.waiting_proxy_name)
async def msg_proxy_name(message: Message, state: FSMContext) -> None:
    if not is_admin(message.from_user.id):
        return

    data = await state.get_data()
    last_msg_id = data.get("last_msg_id")
    if last_msg_id:
        try:
            await message.bot.edit_message_reply_markup(
                chat_id=message.chat.id,
                message_id=last_msg_id,
                reply_markup=None
            )
        except Exception:
            pass

    name = message.text.strip()
    if not name or len(name) > 64:
        sent = await message.answer(
            "❌ Название должно быть от 1 до 64 символов. Введите название:",
            reply_markup=proxy_cancel_kb(),
        )
        await state.update_data(last_msg_id=sent.message_id)
        return

    parsed = data["parsed"]
    proxy = Proxy(
        id=str(uuid4()),
        name=name,
        proxy_type=parsed["ptype"],
        host=parsed["host"],
        port=parsed["port"],
        username=parsed.get("username"),
        password=parsed.get("password"),
    )
    await ProxyRepository().add(proxy)
    await state.clear()
    logger.info(f"Proxy {name} ({proxy.host}:{proxy.port}) added by admin {message.from_user.id}")

    rows = await _proxy_rows()
    await message.answer(
        f"✅ Прокси <b>{name}</b> добавлен!\n\n🌐 <b>Прокси</b> ({len(rows)}):",
        reply_markup=proxy_list_kb(rows),
    )


# ── Proxy assignment (from account detail) ─────────────────────────────────────

@router_proxy.callback_query(F.data.startswith("proxy_assign_list:"))
async def cb_proxy_assign_list(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    account_id = int(callback.data.split(":", 1)[1])
    rows = await _proxy_rows()
    free = [r for r in rows if not r[5]]
    if not free:
        await callback.answer("⚠️ Нет свободных прокси.", show_alert=True)
        return
    await callback.message.edit_text(
        "Выберите прокси для привязки к аккаунту:",
        reply_markup=proxy_assign_list_kb(rows, account_id),
    )
    await callback.answer()


@router_proxy.callback_query(F.data.startswith("proxy_assign:"))
async def cb_proxy_assign(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    # format: proxy_assign:<account_id>:<proxy_id>
    parts = callback.data.split(":")
    account_id = int(parts[1])
    proxy_id = parts[2]
    await ProxyRepository().assign_to_account(account_id, proxy_id)
    await callback.answer("✅ Прокси привязан")

    from app.keyboards import account_detail_kb
    from utils.session_repo import get_session_accounts, mask_phone
    accounts = await get_session_accounts()
    target = next((a for a in accounts if a[0] == account_id), None)
    if target:
        _, phone, status = target
        status_emoji = "✅" if status == "active" else "⚠️"
        proxy = await ProxyRepository().get_by_id(proxy_id)
        proxy_text = f"\n🌐 Прокси: <b>{proxy.name}</b> (<code>{proxy.host}:{proxy.port}</code>)" if proxy else ""
        text = (
            f"Статус: <code>{status}</code> {status_emoji}\n\n"
            f"Номер: <b>{mask_phone(phone)}</b>"
            f"{proxy_text}"
        )
        await callback.message.edit_text(text, reply_markup=account_detail_kb(account_id, proxy_id))


@router_proxy.callback_query(F.data.startswith("proxy_unassign:"))
async def cb_proxy_unassign(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    account_id = int(callback.data.split(":", 1)[1])
    await ProxyRepository().assign_to_account(account_id, None)
    await callback.answer("🔌 Прокси отвязан")
    from app.keyboards import account_detail_kb
    from utils.session_repo import get_session_accounts, mask_phone
    accounts = await get_session_accounts()
    target = next((a for a in accounts if a[0] == account_id), None)
    if target:
        _, phone, status = target
        status_emoji = "✅" if status == "active" else "⚠️"
        text = (
            f"Статус: <code>{status}</code> {status_emoji}\n\n"
            f"Номер: <b>{mask_phone(phone)}</b>"
        )
        await callback.message.edit_text(text, reply_markup=account_detail_kb(account_id, None))

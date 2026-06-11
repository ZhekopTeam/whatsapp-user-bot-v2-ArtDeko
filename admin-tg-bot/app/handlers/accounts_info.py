import asyncio

from aiogram import F, Router
from aiogram.exceptions import TelegramBadRequest
from aiogram.filters import Command
from aiogram.types import CallbackQuery, Message

from app.keyboards import account_detail_kb, accounts_list_kb, main_menu_kb
from config import settings
from utils.logger import logger
from utils.session_repo import delete_session, get_session_accounts, mask_phone
from utils.sheets_sync import sync_accounts

router_info = Router(name="info")


def is_admin(tg_id: int) -> bool:
    return tg_id in settings.admins_list


def _menu_text() -> str:
    return (
        "👋 <b>Прогрев аккаунтов WhatsApp</b>\n\n"
        "📱 Список авторизованных аккаунтов"
    )


async def _accounts_text() -> str:
    accounts = await get_session_accounts()
    if accounts:
        return f"📱 <b>Авторизованные аккаунты</b> ({len(accounts)}):"
    return "📱 Авторизованных аккаунтов пока нет."


@router_info.message(Command("start"))
async def cmd_start(message: Message) -> None:
    if not is_admin(message.from_user.id):
        return
    await message.answer(_menu_text(), reply_markup=main_menu_kb())


@router_info.callback_query(F.data == "menu:main")
async def cb_main_menu(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await callback.message.edit_text(_menu_text(), reply_markup=main_menu_kb())
    await callback.answer()


@router_info.callback_query(F.data == "menu:accounts")
async def cb_accounts(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    accounts = await get_session_accounts()
    await callback.message.edit_text(
        await _accounts_text(),
        reply_markup=accounts_list_kb(accounts),
    )
    await callback.answer()


@router_info.callback_query(F.data.startswith("account:"))
async def cb_account_detail(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    account_id = int(callback.data.split(":", 1)[1])
    accounts = await get_session_accounts()
    target = next((a for a in accounts if a[0] == account_id), None)
    if not target:
        await callback.answer("Аккаунт не найден", show_alert=True)
        return
    _, phone, status = target

    status_emoji = "✅" if status == "active" else "⚠️"
    text = (
        f"Статус: <code>{status}</code> {status_emoji}\n\n"
        f"Номер: <b>{mask_phone(phone)}</b>"
    )
    await callback.message.edit_text(text, reply_markup=account_detail_kb(account_id))
    await callback.answer()


@router_info.callback_query(F.data.startswith("delete_account:"))
async def cb_delete_account(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    account_id = int(callback.data.split(":", 1)[1])
    accounts = await get_session_accounts()
    target = next((a for a in accounts if a[0] == account_id), None)
    if not target:
        await callback.answer("⚠️ Аккаунт не найден", show_alert=True)
        return

    _, phone, _ = target
    deleted = await delete_session(phone)
    if deleted:
        logger.info(
            f"Account {phone} deleted by admin {callback.from_user.id}")
        await callback.answer("✅ Сессия удалена")
    else:
        await callback.answer("⚠️ Не удалось удалить", show_alert=True)

    accounts = await get_session_accounts()
    try:
        await callback.message.edit_text(
            await _accounts_text(),
            reply_markup=accounts_list_kb(accounts),
        )
    except TelegramBadRequest as e:
        if "message is not modified" not in str(e):
            raise

    if deleted:
        asyncio.create_task(sync_accounts())

from aiogram.types import InlineKeyboardMarkup
from aiogram.utils.keyboard import InlineKeyboardBuilder

from utils.session_repo import mask_phone


def main_menu_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="📱 Аккаунты WhatsApp", callback_data="menu:accounts")
    builder.adjust(1)
    return builder.as_markup()


def accounts_list_kb(accounts: list[tuple[int, str, str]]) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for account_id, phone, status in accounts:
        prefix = "⚠️ " if status != "active" else ""
        builder.button(
            text=f"{prefix}{mask_phone(phone)}",
            callback_data=f"account:{account_id}",
        )
    builder.button(text="➕ Добавить аккаунт", callback_data="add_account")
    builder.button(text="← Меню", callback_data="menu:main")
    builder.adjust(1)
    return builder.as_markup()


def account_detail_kb(account_id: int) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(
        text="🗑 Удалить аккаунт",
        callback_data=f"delete_account:{account_id}",
    )
    builder.button(text="← Назад", callback_data="menu:accounts")
    builder.adjust(1)
    return builder.as_markup()


def auth_cancel_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="✖️ Отмена", callback_data="auth:cancel")
    return builder.as_markup()


def back_to_accounts_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="← К списку аккаунтов", callback_data="menu:accounts")
    return builder.as_markup()

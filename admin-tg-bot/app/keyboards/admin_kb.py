from aiogram.types import InlineKeyboardMarkup
from aiogram.utils.keyboard import InlineKeyboardBuilder

from utils.session_repo import mask_phone


def main_menu_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="📱 Аккаунты WhatsApp", callback_data="menu:accounts")
    builder.button(text="🔄 Схемы общения", callback_data="menu:communications")
    builder.adjust(1)
    return builder.as_markup()


def communications_menu_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="➕ Создать цепочку общения", callback_data="comm:create")
    builder.button(text="← Главное меню", callback_data="menu:main")
    builder.adjust(1)
    return builder.as_markup()


def comm_choose_accounts_kb(active_accounts: list[tuple[int, str]], last_selected_id: int | None, can_finish: bool) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for acc_id, phone in active_accounts:
        if acc_id == last_selected_id:
            continue
        builder.button(
            text=mask_phone(phone),
            callback_data=f"comm:add_acc:{acc_id}"
        )
    builder.adjust(1)

    controls = InlineKeyboardBuilder()
    controls.button(text="↩️ Сбросить", callback_data="comm:reset")
    if can_finish:
        controls.button(text="✅ Готово", callback_data="comm:finish")
    controls.button(text="← Отмена", callback_data="menu:communications")
    controls.adjust(2 if can_finish else 1)

    builder.attach(controls)
    return builder.as_markup()


def comm_time_options_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="▶️ Начать сейчас", callback_data="comm:time:now")
    builder.button(text="⏰ Через 10 минут", callback_data="comm:time:10m")
    builder.button(text="⏰ Через 1 час", callback_data="comm:time:1h")
    builder.button(text="📅 Завтра в 09:00", callback_data="comm:time:tomorrow_9")
    builder.button(text="✏️ Ввести вручную", callback_data="comm:time:custom")
    builder.button(text="← Назад", callback_data="comm:reset")
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

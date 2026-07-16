from aiogram.types import InlineKeyboardMarkup
from aiogram.utils.keyboard import InlineKeyboardBuilder

from utils.session_repo import mask_phone


def main_menu_kb(*, show_admins: bool = False) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="📱 Аккаунты WhatsApp", callback_data="menu:accounts")
    builder.button(text="👥 Группы аккаунтов", callback_data="menu:communications")
    builder.button(text="🌐 Прокси", callback_data="menu:proxy")
    if show_admins:
        builder.button(text="👥 Админы", callback_data="menu:admins")
    builder.adjust(1)
    return builder.as_markup()


def communications_menu_kb(groups: list[tuple[int, int]]) -> InlineKeyboardMarkup:
    """groups: list of (group_id, member_count)."""
    builder = InlineKeyboardBuilder()
    for group_id, count in groups:
        builder.button(
            text=f"Группа #{group_id} ({count} акк.)",
            callback_data=f"group:view:{group_id}",
        )
    builder.button(text="➕ Создать группу", callback_data="group:create")
    builder.button(text="← Меню", callback_data="menu:main")
    builder.adjust(1)
    return builder.as_markup()


def group_detail_kb(group_id: int) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(
        text="▶️ Запустить переписку",
        callback_data=f"group:start:{group_id}",
    )
    builder.button(
        text="🗑 Удалить группу",
        callback_data=f"group:del:{group_id}",
    )
    builder.button(text="← Назад", callback_data="menu:communications")
    builder.adjust(1)
    return builder.as_markup()


def group_choose_accounts_kb(
    accounts: list[tuple[int, str]],
    selected_ids: list[int],
) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    selected_set = set(selected_ids)
    for acc_id, phone in accounts:
        mark = "✅ " if acc_id in selected_set else ""
        builder.button(
            text=f"{mark}{mask_phone(phone)}",
            callback_data=f"group:toggle:{acc_id}",
        )
    builder.adjust(1)

    controls = InlineKeyboardBuilder()
    controls.button(text="↩️ Сбросить", callback_data="group:reset")
    if 2 <= len(selected_ids) <= 6:
        controls.button(text="✅ Готово", callback_data="group:finish")
    controls.button(text="← Отмена", callback_data="menu:communications")
    controls.adjust(2 if 2 <= len(selected_ids) <= 6 else 1)

    builder.attach(controls)
    return builder.as_markup()


def group_time_options_kb(group_id: int | None = None) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    prefix = f"group:time:{group_id}:" if group_id is not None else "group:time:new:"
    builder.button(text="▶️ Начать сейчас", callback_data=f"{prefix}now")
    builder.button(text="⏰ Через 10 минут", callback_data=f"{prefix}10m")
    builder.button(text="⏰ Через 1 час", callback_data=f"{prefix}1h")
    builder.button(text="📅 Завтра в 10:00", callback_data=f"{prefix}tomorrow_10")
    builder.button(text="✏️ Ввести вручную", callback_data=f"{prefix}custom")
    back = (
        f"group:view:{group_id}"
        if group_id is not None
        else "menu:communications"
    )
    builder.button(text="← Назад", callback_data=back)
    builder.adjust(1)
    return builder.as_markup()


def group_days_options_kb(group_id: int) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for days in (1, 3, 7, 14, 30):
        label = "1 день" if days == 1 else f"{days} дней"
        builder.button(
            text=label,
            callback_data=f"group:days:{group_id}:{days}",
        )
    builder.button(
        text="✏️ Ввести вручную",
        callback_data=f"group:days:{group_id}:custom",
    )
    builder.button(text="← Назад", callback_data=f"group:start:{group_id}")
    builder.adjust(2, 2, 1, 1, 1)
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


def account_detail_kb(account_id: int, proxy_id: str | None = None) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    if proxy_id:
        builder.button(
            text="🔌 Отвязать прокси",
            callback_data=f"proxy_unassign:{account_id}",
        )
    else:
        builder.button(
            text="🌐 Привязать прокси",
            callback_data=f"proxy_assign_list:{account_id}",
        )
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


def proxy_list_kb(proxies: list[tuple[str, str, str, str, int, int, bool]]) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for proxy_id, name, ptype, host, port, usage_count, is_busy in proxies:
        status = "🔴" if is_busy else "🟢"
        builder.button(
            text=f"{status} {name} ({ptype}://{host}:{port}) [{usage_count}/6]",
            callback_data=f"proxy_detail:{proxy_id}",
        )
    builder.button(text="➕ Добавить прокси", callback_data="proxy_add")
    builder.button(text="← Меню", callback_data="menu:main")
    builder.adjust(1)
    return builder.as_markup()


def proxy_detail_kb(proxy_id: str) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="🗑 Удалить", callback_data=f"proxy_del:{proxy_id}")
    builder.button(text="← Назад", callback_data="menu:proxy")
    builder.adjust(1)
    return builder.as_markup()


def proxy_cancel_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="✖️ Отмена", callback_data="menu:proxy")
    return builder.as_markup()


def proxy_assign_list_kb(
    proxies: list[tuple[str, str, str, str, int, int, bool]],
    account_id: int,
) -> InlineKeyboardMarkup:
    """List of free proxies to assign to an account."""
    builder = InlineKeyboardBuilder()
    for proxy_id, name, ptype, host, port, usage_count, is_busy in proxies:
        if is_busy:
            continue
        builder.button(
            text=f"🟢 {name} ({ptype}://{host}:{port}) [{usage_count}/6]",
            callback_data=f"proxy_assign:{account_id}:{proxy_id}",
        )
    builder.button(text="← Назад", callback_data=f"account:{account_id}")
    builder.adjust(1)
    return builder.as_markup()


def admins_list_kb(db_admins: list[tuple[int, str]]) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for tg_id, label in db_admins:
        builder.button(
            text=f"🗑 {label}",
            callback_data=f"admin_del:{tg_id}",
        )
    builder.button(text="➕ Добавить админа", callback_data="admin_add")
    builder.button(text="← Меню", callback_data="menu:main")
    builder.adjust(1)
    return builder.as_markup()


def admin_cancel_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="✖️ Отмена", callback_data="menu:admins")
    return builder.as_markup()

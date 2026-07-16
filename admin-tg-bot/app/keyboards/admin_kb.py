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


def communications_menu_kb(
    groups: list[tuple[int, str, int]],
    *,
    show_finished: bool = False,
) -> InlineKeyboardMarkup:
    """groups: (group_id, name, member_count) — active only."""
    builder = InlineKeyboardBuilder()
    for group_id, name, count in groups:
        label = name or f"Группа #{group_id}"
        builder.button(
            text=f"🟢 {label} ({count} акк.)",
            callback_data=f"group:view:{group_id}",
        )
    if show_finished:
        builder.button(text="✅ Завершённые", callback_data="group:finished_list")
    builder.button(text="➕ Создать группу", callback_data="group:create")
    builder.button(text="← Меню", callback_data="menu:main")
    builder.adjust(1)
    return builder.as_markup()


def group_finished_list_kb(groups: list[tuple[int, str, int]]) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for group_id, name, count in groups:
        label = name or f"Группа #{group_id}"
        builder.button(
            text=f"✅ {label} ({count} акк.)",
            callback_data=f"group:view:{group_id}",
        )
    builder.button(text="← Назад", callback_data="menu:communications")
    builder.adjust(1)
    return builder.as_markup()


def group_detail_kb(
    group_id: int,
    *,
    status: str = "enabled",
    has_proxy: bool = False,
) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    if status == "finished":
        builder.button(
            text="🏁 Завершить",
            callback_data=f"group:complete:{group_id}",
        )
        builder.button(text="← Назад", callback_data="group:finished_list")
    else:
        if has_proxy:
            builder.button(
                text="🔌 Сменить / отвязать прокси",
                callback_data=f"group:proxy:{group_id}",
            )
        else:
            builder.button(
                text="🌐 Привязать прокси",
                callback_data=f"group:proxy:{group_id}",
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
        controls.button(text="✅ Готово", callback_data="group:accs_done")
    controls.button(text="← Отмена", callback_data="menu:communications")
    controls.adjust(2 if 2 <= len(selected_ids) <= 6 else 1)

    builder.attach(controls)
    return builder.as_markup()


def group_proxy_pick_kb(
    proxies: list[tuple[str, str, str, str, int]],
    *,
    back_callback: str = "menu:communications",
    allow_none: bool = True,
) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for proxy_id, name, ptype, host, port in proxies:
        builder.button(
            text=f"🌐 {name} ({ptype}://{host}:{port})",
            callback_data=f"group:pick_proxy:{proxy_id}",
        )
    if allow_none:
        builder.button(text="🚫 Без прокси", callback_data="group:no_proxy")
    builder.button(text="← Назад", callback_data=back_callback)
    builder.adjust(1)
    return builder.as_markup()


def group_no_proxy_warning_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="🌐 Выбрать прокси", callback_data="group:proxy_back")
    builder.button(
        text="➡️ Продолжить без прокси",
        callback_data="group:no_proxy_confirm",
    )
    builder.adjust(1)
    return builder.as_markup()


def group_proxy_manage_kb(
    group_id: int,
    free_proxies: list[tuple[str, str, str, str, int]],
) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for proxy_id, name, ptype, host, port in free_proxies:
        builder.button(
            text=f"🌐 {name} ({ptype}://{host}:{port})",
            callback_data=f"group:set_proxy:{group_id}:{proxy_id}",
        )
    builder.button(
        text="🔌 Отвязать прокси",
        callback_data=f"group:clear_proxy:{group_id}",
    )
    builder.button(text="← Назад", callback_data=f"group:view:{group_id}")
    builder.adjust(1)
    return builder.as_markup()


def group_time_options_kb() -> InlineKeyboardMarkup:
    """Used during creation (before group exists)."""
    builder = InlineKeyboardBuilder()
    prefix = "group:time:new:"
    builder.button(text="▶️ Начать сейчас", callback_data=f"{prefix}now")
    builder.button(text="⏰ Через 10 минут", callback_data=f"{prefix}10m")
    builder.button(text="⏰ Через 1 час", callback_data=f"{prefix}1h")
    builder.button(text="📅 Завтра в 10:00", callback_data=f"{prefix}tomorrow_10")
    builder.button(text="✏️ Ввести вручную", callback_data=f"{prefix}custom")
    builder.button(text="← Отмена", callback_data="menu:communications")
    builder.adjust(1)
    return builder.as_markup()


def group_days_options_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for days in (1, 3, 7, 14, 30):
        label = "1 день" if days == 1 else f"{days} дней"
        builder.button(text=label, callback_data=f"group:days:new:{days}")
    builder.button(text="✏️ Ввести вручную", callback_data="group:days:new:custom")
    builder.button(text="← Назад", callback_data="group:back_to_time")
    builder.adjust(2, 2, 1, 1, 1)
    return builder.as_markup()


def group_confirm_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="✅ Всё верно", callback_data="group:confirm_ok")
    builder.button(text="✖️ Отмена", callback_data="menu:communications")
    builder.adjust(1)
    return builder.as_markup()


def group_complete_done_kb() -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    builder.button(text="← К списку", callback_data="menu:communications")
    return builder.as_markup()


def accounts_list_kb(accounts: list[tuple[int, str, str]]) -> InlineKeyboardMarkup:
    builder = InlineKeyboardBuilder()
    for account_id, phone, status in accounts:
        if status == "warmup":
            prefix = "🔥 "
        elif status != "active":
            prefix = "⚠️ "
        else:
            prefix = ""
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


def proxy_list_kb(proxies: list[tuple[str, str, str, str, int, bool, str | None]]) -> InlineKeyboardMarkup:
    """proxies: id, name, type, host, port, is_busy, group_label."""
    builder = InlineKeyboardBuilder()
    for proxy_id, name, ptype, host, port, is_busy, group_label in proxies:
        status = "🔴" if is_busy else "🟢"
        suffix = f" → {group_label}" if group_label else ""
        builder.button(
            text=f"{status} {name} ({ptype}://{host}:{port}){suffix}",
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

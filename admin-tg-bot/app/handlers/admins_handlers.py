from aiogram import Bot, F, Router
from aiogram.fsm.context import FSMContext
from aiogram.types import CallbackQuery, InlineKeyboardMarkup, Message

from app.keyboards import admin_cancel_kb, admins_list_kb
from utils.access import (
    add_admin,
    db_admin_ids,
    is_admin,
    is_owner,
    owner_ids,
    remove_admin,
)
from utils.FSM import AddAdmin

router_admins = Router(name="admins")


async def _format_user(bot: Bot, tg_id: int) -> str:
    try:
        user = await bot.get_chat(tg_id)
        if user.username:
            return f"@{user.username}"
        if user.first_name:
            name = user.first_name
            if user.last_name:
                name = f"{name} {user.last_name}"
            return name
    except Exception:
        pass
    return f"<code>{tg_id}</code>"


async def _admin_list_text(bot: Bot) -> str:
    lines = ["👤 <b>Администраторы бота</b>\n"]
    for tg_id in owner_ids():
        label = await _format_user(bot, tg_id)
        lines.append(f"• {label} — владелец")
    for tg_id in db_admin_ids():
        label = await _format_user(bot, tg_id)
        lines.append(f"• {label} — добавлен")
    if not owner_ids() and not db_admin_ids():
        lines.append("Список пуст.")
    lines.append(
        "\nВладельцы задаются разработчиком и не удаляются через бота."
    )
    return "\n".join(lines)


async def _db_admin_buttons(bot: Bot) -> list[tuple[int, str]]:
    buttons: list[tuple[int, str]] = []
    for tg_id in db_admin_ids():
        buttons.append((tg_id, await _format_user(bot, tg_id)))
    return buttons


async def _admins_screen(bot: Bot) -> tuple[str, InlineKeyboardMarkup]:
    buttons = await _db_admin_buttons(bot)
    return await _admin_list_text(bot), admins_list_kb(buttons)


@router_admins.callback_query(F.data == "menu:admins")
async def cb_admins_menu(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_owner(callback.from_user.id):
        await callback.answer("Недостаточно прав", show_alert=True)
        return
    await state.clear()
    text, markup = await _admins_screen(callback.bot)
    await callback.message.edit_text(text, reply_markup=markup)
    await callback.answer()


@router_admins.callback_query(F.data == "admin_add")
async def cb_admin_add(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_owner(callback.from_user.id):
        await callback.answer("Недостаточно прав", show_alert=True)
        return
    await state.set_state(AddAdmin.waiting_tg_id)
    await callback.message.edit_text(
        "Введите Telegram ID нового админа (число):",
        reply_markup=admin_cancel_kb(),
    )
    await callback.answer()


@router_admins.message(AddAdmin.waiting_tg_id)
async def msg_admin_tg_id(message: Message, state: FSMContext) -> None:
    if not is_owner(message.from_user.id):
        return

    raw = (message.text or "").strip()
    if not raw.isdigit():
        await message.answer(
            "⚠️ Некорректный ID. Введите числовой Telegram ID:",
            reply_markup=admin_cancel_kb(),
        )
        return

    tg_id = int(raw)
    if is_owner(tg_id):
        text, markup = await _admins_screen(message.bot)
        await message.answer(
            "⚠️ Этот ID уже является владельцем (из .env).",
            reply_markup=markup,
        )
        await state.clear()
        return

    if is_admin(tg_id):
        text, markup = await _admins_screen(message.bot)
        await message.answer(
            "⚠️ Этот ID уже добавлен как админ.",
            reply_markup=markup,
        )
        await state.clear()
        return

    await add_admin(tg_id)
    await state.clear()
    label = await _format_user(message.bot, tg_id)
    text, markup = await _admins_screen(message.bot)
    await message.answer(
        f"✅ Админ {label} добавлен!",
        reply_markup=markup,
    )


@router_admins.callback_query(F.data.startswith("admin_del:"))
async def cb_admin_del(callback: CallbackQuery) -> None:
    if not is_owner(callback.from_user.id):
        await callback.answer("Недостаточно прав", show_alert=True)
        return

    tg_id = int(callback.data.split(":", 1)[1])
    removed = await remove_admin(tg_id)
    if not removed:
        await callback.answer(
            "Нельзя удалить владельца из .env",
            show_alert=True,
        )
        return

    text, markup = await _admins_screen(callback.bot)
    await callback.message.edit_text(text, reply_markup=markup)
    label = await _format_user(callback.bot, tg_id)
    await callback.answer(f"Админ {label} удалён")

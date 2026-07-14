from aiogram import F, Router
from aiogram.fsm.context import FSMContext
from aiogram.types import CallbackQuery, Message

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


def _admins_text() -> str:
    owners = owner_ids()
    admins = db_admin_ids()
    owners_lines = "\n".join(f"• <code>{tid}</code> (владелец)" for tid in owners) or "—"
    admins_lines = "\n".join(f"• <code>{tid}</code>" for tid in admins) or "—"
    return (
        "👥 <b>Админы</b>\n\n"
        f"<b>Владельцы</b> (из .env, нельзя удалить):\n{owners_lines}\n\n"
        f"<b>Админы</b> (можно удалить):\n{admins_lines}"
    )


@router_admins.callback_query(F.data == "menu:admins")
async def cb_admins_menu(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_owner(callback.from_user.id):
        await callback.answer("Недостаточно прав", show_alert=True)
        return
    await state.clear()
    await callback.message.edit_text(
        _admins_text(),
        reply_markup=admins_list_kb(db_admin_ids()),
    )
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
        await message.answer(
            "⚠️ Этот ID уже является владельцем (из .env).",
            reply_markup=admins_list_kb(db_admin_ids()),
        )
        await state.clear()
        return

    if is_admin(tg_id):
        await message.answer(
            "⚠️ Этот ID уже добавлен как админ.",
            reply_markup=admins_list_kb(db_admin_ids()),
        )
        await state.clear()
        return

    await add_admin(tg_id)
    await state.clear()
    await message.answer(
        f"✅ Админ <code>{tg_id}</code> добавлен!",
        reply_markup=admins_list_kb(db_admin_ids()),
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

    await callback.message.edit_text(
        _admins_text(),
        reply_markup=admins_list_kb(db_admin_ids()),
    )
    await callback.answer(f"Админ {tg_id} удалён")

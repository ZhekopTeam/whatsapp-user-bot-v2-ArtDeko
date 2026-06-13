import asyncio
from datetime import datetime, timedelta, timezone
import json
import os
import random
import aiosqlite
from aiogram import F, Router
from aiogram.fsm.context import FSMContext
from aiogram.types import CallbackQuery, Message
from zoneinfo import ZoneInfo

from app.keyboards import (
    communications_menu_kb,
    comm_choose_accounts_kb,
    comm_time_options_kb,
    main_menu_kb,
)
from config import settings
from utils.database import AccountRepository
from utils.logger import logger
from utils.session_repo import mask_phone
from utils import CreateChain


router_comm = Router(name="comm")


def get_bot_timezone() -> ZoneInfo:
    tz_name = os.getenv("BOT_TIMEZONE", "Europe/Moscow")
    return ZoneInfo(tz_name)


def get_sentences_path() -> str:
    p = "/app/data/sentences_list.json"
    if os.path.exists(p):
        return p
    return "data/sentences_list.json"


def generate_message_from_catalog(sentences_path: str) -> str:
    try:
        with open(sentences_path, "r", encoding="utf-8") as f:
            data = json.load(f)
        sentences = data.get("sentences", [])
        if len(sentences) < 2:
            return "Hey! How are you?"
        first, second = random.sample(sentences, 2)

        def normalize(s: str) -> str:
            s = s.strip()
            if not s:
                return ""
            if s[-1] in (".", "!", "?"):
                return s
            return s + "."

        return f"{normalize(first)} {normalize(second)}"
    except Exception as e:
        logger.error(f"Failed to generate message from catalog: {e}")
        return "Привет! Как твои дела?"


async def insert_jobs_to_db(comm_id: int, run_date: str, jobs: list[dict], db_path: str = settings.RUNTIME_DB_PATH) -> None:
    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            """
            INSERT OR IGNORE INTO communication_runs (comm_id, run_date, status, created_at, updated_at)
            VALUES (?, ?, ?, datetime('now'), datetime('now'))
            """,
            (comm_id, run_date, "planned")
        )
        for job in jobs:
            await db.execute(
                """
                INSERT OR IGNORE INTO message_jobs (
                    comm_id, run_date, step_no, sender_account_id, receiver_account_id,
                    planned_at, status, message_text, attempt_count, last_error, created_at, updated_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, '', datetime('now'), datetime('now'))
                """,
                (
                    job["comm_id"],
                    job["run_date"],
                    job["step_no"],
                    job["sender_account_id"],
                    job["receiver_account_id"],
                    job["planned_at"].strftime("%Y-%m-%d %H:%M:%S"),
                    job["status"],
                    job["message_text"]
                )
            )
        await db.commit()


@router_comm.callback_query(F.data == "menu:communications")
async def cb_communications_menu(callback: CallbackQuery, state: FSMContext) -> None:
    await state.clear()
    await callback.message.edit_text(
        "🔄 <b>Схемы общения</b>\n\n"
        "Здесь вы можете настроить цепочку общения между вашими аккаунтами WhatsApp.",
        reply_markup=communications_menu_kb(),
    )
    await callback.answer()


@router_comm.callback_query(F.data == "comm:create")
async def cb_create_chain(callback: CallbackQuery, state: FSMContext) -> None:
    active_accounts = await AccountRepository().get_active()
    if len(active_accounts) < 2:
        await callback.answer("⚠️ Для создания схемы общения нужно как минимум 2 активных аккаунта!", show_alert=True)
        return

    await state.set_state(CreateChain.choosing_accounts)
    await state.update_data(
        selected_ids=[],
        active_accounts=[(a.id, a.phone) for a in active_accounts]
    )

    await _render_choose_accounts_message(callback.message, [], [(a.id, a.phone) for a in active_accounts])
    await callback.answer()


async def _render_choose_accounts_message(message: Message, selected_ids: list[int], active_accounts: list[tuple[int, str]]) -> None:
    if not selected_ids:
        chain_text = "Цепочка пока пуста.\n\n"
    else:
        chain_parts = []
        for i, acc_id in enumerate(selected_ids):
            phone = next((p for aid, p in active_accounts if aid == acc_id), "???")
            chain_parts.append(f"{i+1}. <b>{mask_phone(phone)}</b>")
        chain_text = "<b>Текущая цепочка:</b>\n" + "\n".join(chain_parts) + "\n\n"

    last_selected_id = selected_ids[-1] if selected_ids else None
    can_finish = len(selected_ids) >= 2

    await message.edit_text(
        f"{chain_text}Выберите следующий аккаунт для добавления в цепочку:",
        reply_markup=comm_choose_accounts_kb(active_accounts, last_selected_id, can_finish)
    )


@router_comm.callback_query(F.data.startswith("comm:add_acc:"))
async def cb_add_acc(callback: CallbackQuery, state: FSMContext) -> None:
    acc_id = int(callback.data.split(":")[-1])
    data = await state.get_data()
    selected_ids = data.get("selected_ids", [])
    active_accounts = data.get("active_accounts", [])

    selected_ids.append(acc_id)
    await state.update_data(selected_ids=selected_ids)

    await _render_choose_accounts_message(callback.message, selected_ids, active_accounts)
    await callback.answer()


@router_comm.callback_query(F.data == "comm:reset")
async def cb_reset_chain(callback: CallbackQuery, state: FSMContext) -> None:
    data = await state.get_data()
    active_accounts = data.get("active_accounts", [])

    await state.update_data(selected_ids=[])
    await _render_choose_accounts_message(callback.message, [], active_accounts)
    await callback.answer("Цепочка сброшена")


@router_comm.callback_query(F.data == "comm:finish")
async def cb_finish_accounts(callback: CallbackQuery, state: FSMContext) -> None:
    data = await state.get_data()
    selected_ids = data.get("selected_ids", [])
    active_accounts = data.get("active_accounts", [])

    if len(selected_ids) < 2:
        await callback.answer("⚠️ Выберите хотя бы 2 аккаунта!", show_alert=True)
        return

    await state.set_state(CreateChain.choosing_start_time)

    chain_parts = []
    for i, acc_id in enumerate(selected_ids):
        phone = next((p for aid, p in active_accounts if aid == acc_id), "???")
        chain_parts.append(f"{i+1}. <b>{mask_phone(phone)}</b>")
    summary = "<b>Создаваемая цепочка:</b>\n" + "\n".join(chain_parts)

    await callback.message.edit_text(
        f"{summary}\n\nВыберите время начала отправки сообщений:",
        reply_markup=comm_time_options_kb()
    )
    await callback.answer()


@router_comm.callback_query(F.data.startswith("comm:time:"))
async def cb_time_option(callback: CallbackQuery, state: FSMContext) -> None:
    option = callback.data.split(":")[-1]
    local_tz = get_bot_timezone()

    if option == "now":
        start_time = datetime.now(timezone.utc)
    elif option == "10m":
        start_time = datetime.now(timezone.utc) + timedelta(minutes=10)
    elif option == "1h":
        start_time = datetime.now(timezone.utc) + timedelta(hours=1)
    elif option == "tomorrow_9":
        local_now = datetime.now(local_tz)
        tomorrow = local_now + timedelta(days=1)
        local_start = datetime(tomorrow.year, tomorrow.month, tomorrow.day, 9, 0, 0, tzinfo=local_tz)
        start_time = local_start.astimezone(timezone.utc)
    elif option == "custom":
        await state.set_state(CreateChain.waiting_custom_time)
        await callback.message.edit_text(
            "Введите дату и время запуска цепочки в формате:\n"
            "<code>ДД.ММ.ГГГГ ЧЧ:ММ</code>\n\n"
            "Пример: <code>15.06.2026 12:00</code>",
            reply_markup=None
        )
        await callback.answer()
        return
    else:
        await callback.answer("Неизвестная опция")
        return

    await callback.message.delete()
    await _create_and_save_chain(callback.message, state, start_time)
    await callback.answer()


@router_comm.message(CreateChain.waiting_custom_time)
async def handle_custom_time(message: Message, state: FSMContext) -> None:
    text = message.text.strip()
    try:
        local_tz = get_bot_timezone()
        parsed = datetime.strptime(text, "%d.%m.%Y %H:%M")
        local_start = parsed.replace(tzinfo=local_tz)

        now_local = datetime.now(local_tz)
        if local_start < now_local:
            await message.answer("⚠️ Время не может быть в прошлом! Попробуйте еще раз:")
            return

        start_time = local_start.astimezone(timezone.utc)
        await _create_and_save_chain(message, state, start_time)
    except ValueError:
        await message.answer("⚠️ Неверный формат даты и времени. Попробуйте еще раз (Пример: 15.06.2026 12:00):")


async def _create_and_save_chain(message: Message, state: FSMContext, start_time: datetime) -> None:
    data = await state.get_data()
    selected_ids = data.get("selected_ids", [])
    active_accounts = data.get("active_accounts", [])

    if len(selected_ids) < 2:
        await message.answer("⚠️ Произошла ошибка: цепочка пуста.")
        await state.clear()
        return

    pairs = []
    for i in range(len(selected_ids) - 1):
        pairs.append((selected_ids[i], selected_ids[i+1]))
        pairs.append((selected_ids[i+1], selected_ids[i]))
    if len(selected_ids) > 2:
        pairs.append((selected_ids[-1], selected_ids[0]))

    comm_id = int(datetime.now(timezone.utc).timestamp())
    run_date = start_time.astimezone(get_bot_timezone()).strftime("%Y-%m-%d")

    jobs = []
    current_time = start_time
    sentences_path = get_sentences_path()

    for step, (sender_id, receiver_id) in enumerate(pairs, start=1):
        if step > 1:
            offset = random.randint(40, 60)
            current_time += timedelta(minutes=offset)

        jobs.append({
            "comm_id": comm_id,
            "run_date": run_date,
            "step_no": step,
            "sender_account_id": sender_id,
            "receiver_account_id": receiver_id,
            "planned_at": current_time,
            "status": "pending",
            "message_text": generate_message_from_catalog(sentences_path)
        })

    try:
        await insert_jobs_to_db(comm_id, run_date, jobs)

        report_lines = [
            f"✅ <b>Цепочка общения успешно создана!</b>",
            f"Task ID: <code>{comm_id}</code>",
            f"Всего сообщений: <b>{len(jobs)}</b>",
            f"Начало отправки: <b>{start_time.astimezone(get_bot_timezone()).strftime('%d.%m.%Y %H:%M')}</b>",
            "\n<b>Шаги цепочки:</b>"
        ]

        for job in jobs:
            sender_phone = next((p for aid, p in active_accounts if aid == job["sender_account_id"]), "???")
            receiver_phone = next((p for aid, p in active_accounts if aid == job["receiver_account_id"]), "???")
            time_str = job["planned_at"].astimezone(get_bot_timezone()).strftime("%H:%M")
            report_lines.append(
                f"• {time_str}: <b>{mask_phone(sender_phone)}</b> ➡️ <b>{mask_phone(receiver_phone)}</b>"
            )

        await message.answer("\n".join(report_lines), reply_markup=main_menu_kb())
    except Exception as e:
        logger.exception("Failed to save communication chain to SQLite")
        await message.answer(f"❌ Ошибка сохранения цепочки в базу данных: {e}", reply_markup=main_menu_kb())

    await state.clear()

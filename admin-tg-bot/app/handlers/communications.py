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
    group_choose_accounts_kb,
    group_complete_done_kb,
    group_confirm_kb,
    group_days_options_kb,
    group_detail_kb,
    group_finished_list_kb,
    group_no_proxy_warning_kb,
    group_proxy_manage_kb,
    group_proxy_pick_kb,
    group_time_options_kb,
    main_menu_kb,
)
from config import settings
from utils.access import is_admin, is_owner
from utils.database import (
    MAX_GROUP_SIZE,
    STATUS_ENABLED,
    STATUS_FINISHED,
    AccountRepository,
    GroupRepository,
    ProxyRepository,
)
from utils.FSM import CreateGroup
from utils.logger import logger
from utils.session_repo import mask_phone

router_comm = Router(name="comm")

REPLY_DELAY_MIN = 12
REPLY_DELAY_MAX = 20
CYCLES_PER_PAIR = 3  # each cycle = A→B then B→A → 6 messages per pair
WINDOW_START_HOUR = 10
WINDOW_END_HOUR = 22


def get_bot_timezone() -> ZoneInfo:
    tz_name = os.getenv("BOT_TIMEZONE", "Europe/Moscow")
    return ZoneInfo(tz_name)


def pair_count(n_accounts: int) -> int:
    if n_accounts < 2:
        return 0
    if n_accounts == 2:
        return 1
    return n_accounts


def messages_per_day(n_accounts: int) -> int:
    return pair_count(n_accounts) * CYCLES_PER_PAIR * 2


def max_dialogue_duration(n_accounts: int) -> timedelta:
    """Worst-case duration (all gaps = REPLY_DELAY_MAX)."""
    gaps = max(messages_per_day(n_accounts) - 1, 0)
    return timedelta(minutes=gaps * REPLY_DELAY_MAX)


def window_bounds_for_day(day_local: datetime) -> tuple[datetime, datetime]:
    """Return (window_start, window_end) in the same tz as day_local."""
    start = day_local.replace(
        hour=WINDOW_START_HOUR, minute=0, second=0, microsecond=0
    )
    end = day_local.replace(
        hour=WINDOW_END_HOUR, minute=0, second=0, microsecond=0
    )
    return start, end


def latest_allowed_start(day_local: datetime, n_accounts: int) -> datetime:
    _, window_end = window_bounds_for_day(day_local)
    return window_end - max_dialogue_duration(n_accounts)


def validate_start_time(
    start_utc: datetime,
    n_accounts: int,
) -> str | None:
    """Return Russian error text if start is outside the send window."""
    local_tz = get_bot_timezone()
    start_local = start_utc.astimezone(local_tz)
    window_start, window_end = window_bounds_for_day(start_local)
    latest = latest_allowed_start(start_local, n_accounts)

    if start_local < window_start:
        return (
            f"Слишком рано.\n"
            f"Окно отправки: <b>{WINDOW_START_HOUR}:00–{WINDOW_END_HOUR}:00</b>.\n"
            f"Самый ранний старт: <b>{window_start.strftime('%d.%m.%Y %H:%M')}</b>"
        )
    if start_local > latest:
        return (
            f"Слишком поздний старт — переписка не успеет до {WINDOW_END_HOUR}:00.\n"
            f"Для {n_accounts} акк. нужно до <b>{latest.strftime('%H:%M')}</b> "
            f"(запас на паузы {REPLY_DELAY_MIN}–{REPLY_DELAY_MAX} мин).\n"
            f"Окно: <b>{WINDOW_START_HOUR}:00–{WINDOW_END_HOUR}:00</b>."
        )
    if start_local >= window_end:
        return (
            f"Окно отправки уже закрыто ({WINDOW_START_HOUR}:00–{WINDOW_END_HOUR}:00)."
        )
    return None


def filter_jobs_within_window(jobs: list[dict], day_local: datetime) -> list[dict]:
    """Drop jobs planned at/after window end (safety net, no reschedule)."""
    _, window_end = window_bounds_for_day(day_local)
    window_end_utc = window_end.astimezone(timezone.utc)
    kept: list[dict] = []
    dropped = 0
    for job in jobs:
        if job["planned_at"] >= window_end_utc:
            dropped += 1
            continue
        kept.append(job)
    if dropped:
        logger.warning(
            "Dropped %s job(s) past %s:00 window for %s",
            dropped,
            WINDOW_END_HOUR,
            day_local.date(),
        )
    # re-number steps after filtering
    for i, job in enumerate(kept, start=1):
        job["step_no"] = i
    return kept


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


def build_dialogue_pairs(account_ids: list[int]) -> list[tuple[int, int]]:
    """Ordered neighbour pairs, closing the circle for 3+ accounts.

    Example for [1,2,3,4,5,6]: (1,2), (2,3), (3,4), (4,5), (5,6), (6,1).
    For 2 accounts: only (1,2).
    """
    n = len(account_ids)
    if n < 2:
        return []
    if n == 2:
        return [(account_ids[0], account_ids[1])]
    pairs: list[tuple[int, int]] = []
    for i in range(n):
        pairs.append((account_ids[i], account_ids[(i + 1) % n]))
    return pairs


def build_group_jobs(
    account_ids: list[int],
    start_time: datetime,
    sentences_path: str,
) -> list[dict]:
    """For each pair: 3× (A→B, B→A) with REPLY_DELAY gaps → 6 messages per pair."""
    pairs = build_dialogue_pairs(account_ids)
    jobs: list[dict] = []
    current_time = start_time
    step = 0

    for pair_idx, (acc_a, acc_b) in enumerate(pairs):
        for _cycle in range(CYCLES_PER_PAIR):
            for sender, receiver in ((acc_a, acc_b), (acc_b, acc_a)):
                if step > 0:
                    current_time += timedelta(
                        minutes=random.randint(REPLY_DELAY_MIN, REPLY_DELAY_MAX)
                    )
                step += 1
                jobs.append({
                    "step_no": step,
                    "sender_account_id": sender,
                    "receiver_account_id": receiver,
                    "planned_at": current_time,
                    "status": "pending",
                    "message_text": generate_message_from_catalog(sentences_path),
                    "pair_idx": pair_idx,
                })
    return jobs


async def insert_jobs_to_db(
    comm_id: int,
    run_date: str,
    jobs: list[dict],
    db_path: str = settings.RUNTIME_DB_PATH,
) -> None:
    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            """
            INSERT OR IGNORE INTO communication_runs
                (comm_id, run_date, status, created_at, updated_at)
            VALUES (?, ?, ?, datetime('now'), datetime('now'))
            """,
            (comm_id, run_date, "planned"),
        )
        for job in jobs:
            await db.execute(
                """
                INSERT OR IGNORE INTO message_jobs (
                    comm_id, run_date, step_no, sender_account_id, receiver_account_id,
                    planned_at, status, message_text, attempt_count, last_error,
                    created_at, updated_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, '', datetime('now'), datetime('now'))
                """,
                (
                    comm_id,
                    run_date,
                    job["step_no"],
                    job["sender_account_id"],
                    job["receiver_account_id"],
                    job["planned_at"].strftime("%Y-%m-%d %H:%M:%S"),
                    job["status"],
                    job["message_text"],
                ),
            )
        await db.commit()


async def cancel_jobs_for_comm(
    comm_id: int,
    db_path: str = settings.RUNTIME_DB_PATH,
) -> None:
    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            """
            UPDATE message_jobs
            SET status = 'cancelled', updated_at = datetime('now')
            WHERE comm_id = ? AND status IN ('pending', 'sending')
            """,
            (comm_id,),
        )
        await db.commit()


async def validate_group_proxies(
    account_ids: list[int],
    *,
    exclude_group_id: int | None = None,
    proxy_id: str | None = None,
) -> str | None:
    """Validate proxy uniqueness for a group.

    Proxy is bound to the group (not to accounts). One proxy → one group.
    """
    if proxy_id is None and exclude_group_id is not None:
        group = await GroupRepository().get_by_id(exclude_group_id)
        proxy_id = group.proxy_id if group else None

    if not proxy_id:
        return None

    owner = await GroupRepository().get_group_id_for_proxy(proxy_id)
    if owner is not None and owner != exclude_group_id:
        return (
            "Этот прокси уже привязан к другой группе.\n"
            "Один прокси — только одна группа."
        )
    return None


async def _free_proxy_rows() -> list[tuple[str, str, str, str, int]]:
    repo = ProxyRepository()
    used = await GroupRepository().list_used_proxy_ids()
    free: list[tuple[str, str, str, str, int]] = []
    for p in await repo.get_all():
        if p.id in used:
            continue
        free.append((p.id, p.name, p.proxy_type, p.host, p.port))
    return free


def _end_date_for_start(start_time: datetime, days: int) -> str:
    local_tz = get_bot_timezone()
    local_start = start_time.astimezone(local_tz)
    last_day = local_start.replace(
        hour=WINDOW_START_HOUR, minute=0, second=0, microsecond=0
    ) + timedelta(days=days - 1)
    return last_day.strftime("%Y-%m-%d")


def _status_label(status: str) -> str:
    if status == STATUS_FINISHED:
        return "завершена"
    return "активна"


async def _groups_menu_payload() -> tuple[str, object]:
    group_repo = GroupRepository()
    local_tz = get_bot_timezone()
    today = datetime.now(local_tz).strftime("%Y-%m-%d")
    await group_repo.auto_finish_expired(today)

    enabled = await group_repo.get_by_status(STATUS_ENABLED)
    finished = await group_repo.get_by_status(STATUS_FINISHED)

    rows: list[tuple[int, str, int]] = []
    for g in enabled:
        members = await group_repo.get_members(g.id)
        rows.append((g.id, g.name or f"Группа #{g.id}", len(members)))

    text = (
        "👥 <b>Группы прогрева</b>\n\n"
        "Создайте группу (от 2 до 6 аккаунтов) — "
        "бот сразу запланирует переписку на выбранные дни.\n\n"
        f"⏱ Окно отправки: <b>{WINDOW_START_HOUR}:00–{WINDOW_END_HOUR}:00</b>\n"
        f"Паузы между сообщениями: <b>{REPLY_DELAY_MIN}–{REPLY_DELAY_MAX}</b> мин.\n"
        f"Со 2-го дня старт в <b>{WINDOW_START_HOUR}:00</b>.\n\n"
        "🌐 Прокси привязывается к <b>группе</b> (не к аккаунту).\n"
        "Один прокси — только одна группа."
    )
    if not rows:
        text += "\n\nАктивных групп пока нет."
    return text, communications_menu_kb(rows, show_finished=bool(finished))


def _choose_accounts_text(selected_ids: list[int], name: str = "") -> str:
    n = len(selected_ids)
    order = (
        f"Порядок: {' → '.join(str(i) for i in range(1, n + 1))}\n"
        if selected_ids
        else ""
    )
    title = f"«{name}»" if name else "Новая группа"
    return (
        f"👥 <b>{title}</b> ({n}/{MAX_GROUP_SIZE})\n\n"
        f"{order}"
        "Отметьте аккаунты в нужном порядке (от 2 до 6).\n"
        "Порядок важен: переписка идёт 1↔2, 2↔3, … по кругу."
    )


def _parse_time_option(option: str) -> datetime | str | None:
    """Return UTC start time, 'custom', or None if unknown."""
    local_tz = get_bot_timezone()
    if option == "now":
        return datetime.now(timezone.utc)
    if option == "10m":
        return datetime.now(timezone.utc) + timedelta(minutes=10)
    if option == "1h":
        return datetime.now(timezone.utc) + timedelta(hours=1)
    if option == "tomorrow_10":
        local_now = datetime.now(local_tz)
        tomorrow = local_now + timedelta(days=1)
        local_start = datetime(
            tomorrow.year, tomorrow.month, tomorrow.day,
            WINDOW_START_HOUR, 0, 0, tzinfo=local_tz,
        )
        return local_start.astimezone(timezone.utc)
    if option == "custom":
        return "custom"
    return None


def _phone_map_from_available(available: list) -> dict[int, str]:
    return {int(acc_id): phone for acc_id, phone in available}


async def _build_preview_text(data: dict) -> str:
    name = data.get("name") or "Группа"
    selected_ids: list[int] = list(data.get("selected_ids", []))
    available = data.get("available", [])
    phone_by_id = _phone_map_from_available(available)
    start_iso = data.get("start_time_iso")
    days = int(data.get("days") or 1)
    proxy_id = data.get("proxy_id")

    start_time = datetime.fromisoformat(start_iso)
    if start_time.tzinfo is None:
        start_time = start_time.replace(tzinfo=timezone.utc)
    local_tz = get_bot_timezone()
    local_str = start_time.astimezone(local_tz).strftime("%d.%m.%Y %H:%M")
    end_date = _end_date_for_start(start_time, days)
    end_display = datetime.strptime(end_date, "%Y-%m-%d").strftime("%d.%m.%Y")

    accounts_lines = "\n".join(
        f"  {i}. <b>{mask_phone(phone_by_id.get(acc_id, '?'))}</b>"
        for i, acc_id in enumerate(selected_ids, start=1)
    )

    if proxy_id:
        proxy = await ProxyRepository().get_by_id(proxy_id)
        if proxy:
            proxy_line = (
                f"🌐 Прокси: <b>{proxy.name}</b> "
                f"(<code>{proxy.host}:{proxy.port}</code>)"
            )
        else:
            proxy_line = "🌐 Прокси: выбран (не найден в базе)"
    else:
        proxy_line = "🌐 Прокси: без прокси"

    return (
        "📋 <b>Проверьте данные группы:</b>\n\n"
        f"• Название: <b>{name}</b>\n"
        f"• Аккаунты ({len(selected_ids)}):\n{accounts_lines}\n"
        f"• Старт: <b>{local_str}</b>\n"
        f"• Дней: <b>{days}</b> (до <b>{end_display}</b>)\n"
        f"• {proxy_line}\n"
        f"• Окно <b>{WINDOW_START_HOUR}:00–{WINDOW_END_HOUR}:00</b>, "
        f"паузы <b>{REPLY_DELAY_MIN}–{REPLY_DELAY_MAX}</b> мин"
    )


async def _ask_days_new(
    message: Message,
    state: FSMContext,
    start_time: datetime,
    *,
    edit: bool,
) -> None:
    await state.set_state(CreateGroup.choosing_days)
    await state.update_data(start_time_iso=start_time.isoformat())
    local_str = start_time.astimezone(get_bot_timezone()).strftime("%d.%m.%Y %H:%M")
    text = (
        f"Старт: <b>{local_str}</b>\n"
        f"Окно: <b>{WINDOW_START_HOUR}:00–{WINDOW_END_HOUR}:00</b>\n\n"
        "На сколько дней запланировать переписку?\n"
        f"1-й день — в выбранное время, со 2-го — в "
        f"<b>{WINDOW_START_HOUR}:00</b>.\n"
        f"Сообщения после {WINDOW_END_HOUR}:00 отменяются (не переносятся)."
    )
    markup = group_days_options_kb()
    if edit:
        await message.edit_text(text, reply_markup=markup)
    else:
        await message.answer(text, reply_markup=markup)


async def _ask_proxy(message: Message, state: FSMContext, *, edit: bool) -> None:
    free = await _free_proxy_rows()
    await state.set_state(CreateGroup.choosing_proxy)
    text = (
        "🌐 <b>Выберите прокси для группы</b>\n\n"
        "Один прокси — только одна группа (до 6 аккаунтов).\n"
        "Прокси применяется ко всем аккаунтам группы."
    )
    markup = group_proxy_pick_kb(free)
    if edit:
        await message.edit_text(text, reply_markup=markup)
    else:
        await message.answer(text, reply_markup=markup)


async def _show_confirm(message: Message, state: FSMContext, *, edit: bool) -> None:
    data = await state.get_data()
    await state.set_state(CreateGroup.confirming)
    text = await _build_preview_text(data)
    markup = group_confirm_kb()
    if edit:
        await message.edit_text(text, reply_markup=markup)
    else:
        await message.answer(text, reply_markup=markup)


async def _create_and_schedule(
    message: Message,
    state: FSMContext,
) -> None:
    data = await state.get_data()
    name = (data.get("name") or "").strip() or "Группа"
    selected_ids: list[int] = list(data.get("selected_ids", []))
    available = data.get("available", [])
    phone_by_id = _phone_map_from_available(available)
    start_iso = data.get("start_time_iso")
    days = int(data.get("days") or 1)
    proxy_id: str | None = data.get("proxy_id")

    if not 2 <= len(selected_ids) <= MAX_GROUP_SIZE:
        await message.answer(
            f"⚠️ Выберите от 2 до {MAX_GROUP_SIZE} аккаунтов.",
            reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
        )
        await state.clear()
        return

    if not start_iso:
        await message.answer(
            "⚠️ Не выбрано время старта. Начните заново.",
            reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
        )
        await state.clear()
        return

    start_time = datetime.fromisoformat(start_iso)
    if start_time.tzinfo is None:
        start_time = start_time.replace(tzinfo=timezone.utc)

    start_err = validate_start_time(start_time, len(selected_ids))
    if start_err:
        await message.answer(
            f"⚠️ {start_err}",
            reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
        )
        await state.clear()
        return

    proxy_err = await validate_group_proxies(selected_ids, proxy_id=proxy_id)
    if proxy_err:
        await message.answer(
            f"⚠️ {proxy_err}",
            reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
        )
        await state.clear()
        return

    local_tz = get_bot_timezone()
    local_start = start_time.astimezone(local_tz)

    # Day 2+ always starts at window open (10:00)
    if days > 1:
        day2_local = (local_start + timedelta(days=1)).replace(
            hour=WINDOW_START_HOUR, minute=0, second=0, microsecond=0
        )
        day2_err = validate_start_time(
            day2_local.astimezone(timezone.utc), len(selected_ids)
        )
        if day2_err:
            await message.answer(
                f"⚠️ Со 2-го дня старт в {WINDOW_START_HOUR}:00 невозможен:\n{day2_err}",
                reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
            )
            await state.clear()
            return

    end_date = _end_date_for_start(start_time, days)
    group_repo = GroupRepository()

    try:
        group = await group_repo.create(
            selected_ids,
            name=name,
            proxy_id=proxy_id,
            start_at=start_time,
            days=days,
            end_date=end_date,
            status=STATUS_ENABLED,
        )
    except Exception as e:
        logger.exception("Failed to create account group")
        await message.answer(
            f"❌ Ошибка создания группы: {e}",
            reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
        )
        await state.clear()
        return

    sentences_path = get_sentences_path()
    comm_id = int(datetime.now(timezone.utc).timestamp())
    all_jobs: list[dict] = []
    dropped_total = 0

    try:
        for day_offset in range(days):
            if day_offset == 0:
                day_start = start_time
                day_local = local_start
            else:
                day_local = local_start.replace(
                    hour=WINDOW_START_HOUR, minute=0, second=0, microsecond=0
                ) + timedelta(days=day_offset)
                day_start = day_local.astimezone(timezone.utc)

            run_date = day_local.strftime("%Y-%m-%d")
            day_jobs = build_group_jobs(selected_ids, day_start, sentences_path)
            before = len(day_jobs)
            day_jobs = filter_jobs_within_window(day_jobs, day_local)
            dropped_total += before - len(day_jobs)
            if not day_jobs:
                logger.warning(
                    "No jobs left within window for group %s day %s",
                    group.id,
                    run_date,
                )
                continue
            for job in day_jobs:
                job["comm_id"] = comm_id
                job["run_date"] = run_date
            await insert_jobs_to_db(comm_id, run_date, day_jobs)
            all_jobs.extend(day_jobs)

        if not all_jobs:
            await group_repo.delete(group.id)
            await message.answer(
                "⚠️ Не удалось создать сообщения в окне отправки. "
                "Выберите более раннее время старта.",
                reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
            )
            await state.clear()
            return

        await group_repo.set_comm_id(group.id, comm_id)

        pairs = build_dialogue_pairs(selected_ids)
        days_created = len({j["run_date"] for j in all_jobs})
        end_display = datetime.strptime(end_date, "%Y-%m-%d").strftime("%d.%m.%Y")
        report = [
            f"✅ <b>Группа «{name}» создана и запущена!</b>",
            f"ID группы: <b>#{group.id}</b>",
            f"Task ID: <code>{comm_id}</code>",
            f"Дней: <b>{days_created}</b> · пар/день: <b>{len(pairs)}</b> · "
            f"всего сообщений: <b>{len(all_jobs)}</b>",
            f"Окно: <b>{WINDOW_START_HOUR}:00–{WINDOW_END_HOUR}:00</b>",
            f"Начало: <b>{local_start.strftime('%d.%m.%Y %H:%M')}</b>",
            f"До: <b>{end_display}</b> включительно",
        ]
        if days > 1:
            report.append(
                f"Со 2-го дня: каждый день в <b>{WINDOW_START_HOUR}:00</b>"
            )
        if dropped_total:
            report.append(
                f"⚠️ Отброшено вне окна: <b>{dropped_total}</b> "
                f"(не переносятся на следующий день)"
            )
        report.append("\n<b>Расписание (первые шаги 1-го дня):</b>")
        first_day = all_jobs[0]["run_date"]
        first_day_jobs = [j for j in all_jobs if j["run_date"] == first_day]
        for job in first_day_jobs[:12]:
            s = mask_phone(phone_by_id.get(job["sender_account_id"], "?"))
            r = mask_phone(phone_by_id.get(job["receiver_account_id"], "?"))
            t = job["planned_at"].astimezone(local_tz).strftime("%H:%M")
            report.append(f"• {t}: <b>{s}</b> ➡️ <b>{r}</b>")
        if len(first_day_jobs) > 12:
            report.append(f"… и ещё {len(first_day_jobs) - 12} сообщений в 1-й день")

        await message.answer(
            "\n".join(report),
            reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
        )
    except Exception as e:
        logger.exception("Failed to save group dialogue jobs")
        await message.answer(
            f"❌ Ошибка сохранения в базу: {e}",
            reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
        )

    await state.clear()


# ── Menu / list ───────────────────────────────────────────────────────────────


@router_comm.callback_query(F.data == "menu:communications")
async def cb_groups_menu(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await state.clear()
    text, markup = await _groups_menu_payload()
    await callback.message.edit_text(text, reply_markup=markup)
    await callback.answer()


@router_comm.callback_query(F.data == "group:finished_list")
async def cb_finished_list(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    group_repo = GroupRepository()
    finished = await group_repo.get_by_status(STATUS_FINISHED)
    rows: list[tuple[int, str, int]] = []
    for g in finished:
        members = await group_repo.get_members(g.id)
        rows.append((g.id, g.name or f"Группа #{g.id}", len(members)))
    if not rows:
        text = "✅ Завершённых групп пока нет."
    else:
        text = "✅ <b>Завершённые группы</b>\n\nВыберите группу:"
    await callback.message.edit_text(text, reply_markup=group_finished_list_kb(rows))
    await callback.answer()


@router_comm.callback_query(F.data.startswith("group:view:"))
async def cb_group_view(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    group_id = int(callback.data.split(":")[-1])
    group_repo = GroupRepository()
    group = await group_repo.get_by_id(group_id)
    if not group:
        await callback.answer("Группа не найдена", show_alert=True)
        return
    members = await group_repo.get_members(group_id)
    title = group.name or f"Группа #{group_id}"
    lines = [f"👥 <b>{title}</b> (#{group_id})\n"]
    for i, acc in enumerate(members, start=1):
        lines.append(f"{i}. <b>{mask_phone(acc.phone)}</b>")

    proxy_line = "🌐 Прокси: не привязан"
    if group.proxy_id:
        proxy = await ProxyRepository().get_by_id(group.proxy_id)
        if proxy:
            proxy_line = (
                f"🌐 Прокси: <b>{proxy.name}</b> "
                f"(<code>{proxy.host}:{proxy.port}</code>)"
            )
    lines.append(f"\n{proxy_line}")

    local_tz = get_bot_timezone()
    if group.start_at:
        start_at = group.start_at
        if start_at.tzinfo is None:
            start_at = start_at.replace(tzinfo=timezone.utc)
        start_str = start_at.astimezone(local_tz).strftime("%d.%m.%Y %H:%M")
    else:
        start_str = "—"
    days = group.days or 1
    end_str = group.end_date or "—"
    if group.end_date:
        try:
            end_str = datetime.strptime(group.end_date, "%Y-%m-%d").strftime("%d.%m.%Y")
        except ValueError:
            end_str = group.end_date

    lines.append(f"📅 Старт: <b>{start_str}</b>")
    lines.append(f"📆 Дней: <b>{days}</b> (до <b>{end_str}</b>)")
    lines.append(f"📌 Статус: <b>{_status_label(group.status)}</b>")
    lines.append(
        "\nПереписка идёт парами по кругу:\n"
        "1↔2, 2↔3, …, последний↔первый.\n"
        f"В каждой паре — 6 сообщений (3 цикла туда-обратно, "
        f"пауза {REPLY_DELAY_MIN}–{REPLY_DELAY_MAX} мин).\n"
        f"Окно отправки: {WINDOW_START_HOUR}:00–{WINDOW_END_HOUR}:00."
    )
    await callback.message.edit_text(
        "\n".join(lines),
        reply_markup=group_detail_kb(
            group_id,
            status=group.status,
            has_proxy=bool(group.proxy_id),
        ),
    )
    await callback.answer()


@router_comm.callback_query(F.data.startswith("group:del:"))
async def cb_group_del(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    group_id = int(callback.data.split(":")[-1])
    group_repo = GroupRepository()
    group = await group_repo.get_by_id(group_id)
    if group and group.comm_id:
        await cancel_jobs_for_comm(group.comm_id)
    await group_repo.delete(group_id)
    text, markup = await _groups_menu_payload()
    await callback.message.edit_text(text, reply_markup=markup)
    await callback.answer("Группа удалена")


@router_comm.callback_query(F.data.startswith("group:complete:"))
async def cb_group_complete(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    group_id = int(callback.data.split(":")[-1])
    group_repo = GroupRepository()
    group = await group_repo.get_by_id(group_id)
    if not group:
        await callback.answer("Группа не найдена", show_alert=True)
        return

    if group.comm_id:
        await cancel_jobs_for_comm(group.comm_id)
    await group_repo.delete(group_id)

    title = group.name or f"Группа #{group_id}"
    await callback.message.edit_text(
        f"🏁 Прогрев «{title}» завершён.\n"
        "Ожидающие сообщения отменены, аккаунты освобождены.",
        reply_markup=group_complete_done_kb(),
    )
    await callback.answer()


# ── Create group: name → accounts → time → days → proxy → confirm ─────────────


@router_comm.callback_query(F.data == "group:create")
async def cb_group_create(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return

    group_repo = GroupRepository()
    taken = await group_repo.list_account_ids_in_active_groups()
    active = await AccountRepository().get_active()
    available = [(a.id, a.phone) for a in active if a.id not in taken]

    if len(available) < 2:
        await callback.answer(
            "⚠️ Нужно минимум 2 свободных активных аккаунта "
            "(не состоящих в других активных группах).",
            show_alert=True,
        )
        return

    await state.set_state(CreateGroup.waiting_name)
    await state.update_data(selected_ids=[], available=available, proxy_id=None)

    await callback.message.edit_text("Введите название группы прогрева:")
    await callback.answer()


@router_comm.message(CreateGroup.waiting_name)
async def handle_group_name(message: Message, state: FSMContext) -> None:
    if not is_admin(message.from_user.id):
        return
    name = (message.text or "").strip()
    if not 1 <= len(name) <= 64:
        await message.answer(
            "⚠️ Название должно быть от 1 до 64 символов. Попробуйте ещё раз:"
        )
        return

    data = await state.get_data()
    available = data.get("available", [])
    if len(available) < 2:
        group_repo = GroupRepository()
        taken = await group_repo.list_account_ids_in_active_groups()
        active = await AccountRepository().get_active()
        available = [(a.id, a.phone) for a in active if a.id not in taken]
        if len(available) < 2:
            await message.answer(
                "⚠️ Нужно минимум 2 свободных активных аккаунта.",
                reply_markup=main_menu_kb(show_admins=is_owner(message.from_user.id)),
            )
            await state.clear()
            return

    await state.set_state(CreateGroup.choosing_accounts)
    await state.update_data(name=name, selected_ids=[], available=available)

    await message.answer(
        _choose_accounts_text([], name),
        reply_markup=group_choose_accounts_kb(available, []),
    )


@router_comm.callback_query(F.data.startswith("group:toggle:"))
async def cb_group_toggle(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    acc_id = int(callback.data.split(":")[-1])
    data = await state.get_data()
    selected: list[int] = list(data.get("selected_ids", []))
    available = data.get("available", [])
    name = data.get("name", "")

    if acc_id in selected:
        selected.remove(acc_id)
    else:
        if len(selected) >= MAX_GROUP_SIZE:
            await callback.answer(
                f"В группе максимум {MAX_GROUP_SIZE} аккаунтов",
                show_alert=True,
            )
            return
        selected.append(acc_id)

    await state.update_data(selected_ids=selected)
    await callback.message.edit_text(
        _choose_accounts_text(selected, name),
        reply_markup=group_choose_accounts_kb(available, selected),
    )
    await callback.answer()


@router_comm.callback_query(F.data == "group:reset")
async def cb_group_reset(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    data = await state.get_data()
    available = data.get("available", [])
    name = data.get("name", "")
    await state.update_data(selected_ids=[])
    await callback.message.edit_text(
        _choose_accounts_text([], name),
        reply_markup=group_choose_accounts_kb(available, []),
    )
    await callback.answer("Выбор сброшен")


@router_comm.callback_query(F.data == "group:accs_done")
async def cb_group_accs_done(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    data = await state.get_data()
    selected_ids: list[int] = data.get("selected_ids", [])

    if not 2 <= len(selected_ids) <= MAX_GROUP_SIZE:
        await callback.answer(
            f"Выберите от 2 до {MAX_GROUP_SIZE} аккаунтов",
            show_alert=True,
        )
        return

    await state.set_state(CreateGroup.choosing_start_time)
    await state.update_data(selected_ids=selected_ids)

    await callback.message.edit_text(
        "Выберите время запуска переписки:",
        reply_markup=group_time_options_kb(),
    )
    await callback.answer()


@router_comm.callback_query(F.data == "group:back_to_time")
async def cb_group_back_to_time(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await state.set_state(CreateGroup.choosing_start_time)
    await callback.message.edit_text(
        "Выберите время запуска переписки:",
        reply_markup=group_time_options_kb(),
    )
    await callback.answer()


# ── Time selection (creation only: group:time:new:…) ─────────────────────────


@router_comm.callback_query(F.data.startswith("group:time:"))
async def cb_group_time(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    # group:time:new:<option>
    parts = callback.data.split(":")
    if len(parts) < 4 or parts[2] != "new":
        await callback.answer("Неизвестная опция")
        return
    option = parts[3]

    data = await state.get_data()
    selected_ids: list[int] = list(data.get("selected_ids", []))
    if len(selected_ids) < 2:
        await callback.answer("Сначала выберите аккаунты", show_alert=True)
        return

    parsed = _parse_time_option(option)
    if parsed == "custom":
        await state.set_state(CreateGroup.waiting_custom_time)
        await callback.message.edit_text(
            "Введите дату и время запуска в формате:\n"
            "<code>ДД.ММ.ГГГГ ЧЧ:ММ</code>\n\n"
            "Пример: <code>15.06.2026 12:00</code>",
            reply_markup=None,
        )
        await callback.answer()
        return
    if parsed is None:
        await callback.answer("Неизвестная опция")
        return

    err = validate_start_time(parsed, len(selected_ids))
    if err:
        await callback.answer(
            err.replace("<b>", "").replace("</b>", ""),
            show_alert=True,
        )
        return

    await _ask_days_new(callback.message, state, parsed, edit=True)
    await callback.answer()


@router_comm.message(CreateGroup.waiting_custom_time)
async def handle_custom_time(message: Message, state: FSMContext) -> None:
    if not is_admin(message.from_user.id):
        return
    text = (message.text or "").strip()
    try:
        local_tz = get_bot_timezone()
        parsed = datetime.strptime(text, "%d.%m.%Y %H:%M")
        local_start = parsed.replace(tzinfo=local_tz)
        if local_start < datetime.now(local_tz):
            await message.answer(
                "⚠️ Время не может быть в прошлом! Попробуйте ещё раз:"
            )
            return
        start_time = local_start.astimezone(timezone.utc)
        data = await state.get_data()
        selected_ids: list[int] = list(data.get("selected_ids", []))
        if len(selected_ids) < 2:
            await message.answer("⚠️ Аккаунты не выбраны.")
            await state.clear()
            return
        err = validate_start_time(start_time, len(selected_ids))
        if err:
            await message.answer(f"⚠️ {err}")
            return
        await _ask_days_new(message, state, start_time, edit=False)
    except ValueError:
        await message.answer(
            "⚠️ Неверный формат. Пример: <code>15.06.2026 12:00</code>"
        )


# ── Days selection (creation only: group:days:new:…) ─────────────────────────


@router_comm.callback_query(F.data.startswith("group:days:"))
async def cb_group_days(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return

    # group:days:new:<N|custom>
    parts = callback.data.split(":")
    if len(parts) < 4 or parts[2] != "new":
        await callback.answer("Неизвестная опция")
        return

    option = parts[3]
    data = await state.get_data()
    start_iso = data.get("start_time_iso")
    if not start_iso:
        await callback.answer("Сначала выберите время старта", show_alert=True)
        return

    if option == "custom":
        await state.set_state(CreateGroup.waiting_custom_days)
        await callback.message.edit_text(
            "Введите количество дней числом (от 1 до 90):\n"
            "Пример: <code>14</code>",
            reply_markup=None,
        )
        await callback.answer()
        return

    try:
        days = int(option)
    except ValueError:
        await callback.answer("Неизвестная опция")
        return

    if not 1 <= days <= 90:
        await callback.answer("Допустимо от 1 до 90 дней", show_alert=True)
        return

    await state.update_data(days=days)
    await _ask_proxy(callback.message, state, edit=True)
    await callback.answer()


@router_comm.message(CreateGroup.waiting_custom_days)
async def handle_custom_days(message: Message, state: FSMContext) -> None:
    if not is_admin(message.from_user.id):
        return
    raw = (message.text or "").strip()
    if not raw.isdigit():
        await message.answer("⚠️ Введите целое число дней, например: <code>14</code>")
        return

    days = int(raw)
    if not 1 <= days <= 90:
        await message.answer("⚠️ Допустимо от 1 до 90 дней. Попробуйте ещё раз:")
        return

    data = await state.get_data()
    if not data.get("start_time_iso"):
        await message.answer("⚠️ Не хватает данных. Начните заново.")
        await state.clear()
        return

    await state.update_data(days=days)
    await _ask_proxy(message, state, edit=False)


# ── Proxy selection (during creation) ─────────────────────────────────────────


@router_comm.callback_query(F.data.startswith("group:pick_proxy:"))
async def cb_group_pick_proxy(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    proxy_id = callback.data.split(":")[-1]
    used = await GroupRepository().list_used_proxy_ids()
    if proxy_id in used:
        await callback.answer(
            "Этот прокси уже занят другой группой",
            show_alert=True,
        )
        return
    await state.update_data(proxy_id=proxy_id)
    await _show_confirm(callback.message, state, edit=True)
    await callback.answer()


@router_comm.callback_query(F.data == "group:no_proxy")
async def cb_group_no_proxy(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await callback.message.edit_text(
        "⚠️ <b>Продолжить без прокси?</b>\n\n"
        "Без прокси аккаунты группы будут ходить с IP сервера.\n"
        "Рекомендуется выбрать отдельный прокси на группу.",
        reply_markup=group_no_proxy_warning_kb(),
    )
    await callback.answer()


@router_comm.callback_query(F.data == "group:proxy_back")
async def cb_group_proxy_back(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await _ask_proxy(callback.message, state, edit=True)
    await callback.answer()


@router_comm.callback_query(F.data == "group:no_proxy_confirm")
async def cb_group_no_proxy_confirm(
    callback: CallbackQuery, state: FSMContext
) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await state.update_data(proxy_id=None)
    await _show_confirm(callback.message, state, edit=True)
    await callback.answer()


@router_comm.callback_query(F.data == "group:confirm_ok")
async def cb_group_confirm_ok(callback: CallbackQuery, state: FSMContext) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    await callback.message.edit_text("⏳ Создаём группу и планируем переписку…")
    await callback.answer()
    await _create_and_schedule(callback.message, state)


# ── Manage proxy on existing group ────────────────────────────────────────────


@router_comm.callback_query(F.data.startswith("group:proxy:"))
async def cb_group_proxy_manage(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    group_id = int(callback.data.split(":")[-1])
    group = await GroupRepository().get_by_id(group_id)
    if not group:
        await callback.answer("Группа не найдена", show_alert=True)
        return

    free = await _free_proxy_rows()
    # Current group's proxy is "used" by this group — allow re-selecting it
    if group.proxy_id:
        proxy = await ProxyRepository().get_by_id(group.proxy_id)
        if proxy and all(p[0] != proxy.id for p in free):
            free.insert(
                0,
                (proxy.id, proxy.name, proxy.proxy_type, proxy.host, proxy.port),
            )

    current = "не привязан"
    if group.proxy_id:
        p = await ProxyRepository().get_by_id(group.proxy_id)
        if p:
            current = f"<b>{p.name}</b> (<code>{p.host}:{p.port}</code>)"

    title = group.name or f"Группа #{group_id}"
    await callback.message.edit_text(
        f"🌐 Прокси «{title}»\n"
        f"Сейчас: {current}\n\n"
        "Один прокси — только одна группа.",
        reply_markup=group_proxy_manage_kb(group_id, free),
    )
    await callback.answer()


@router_comm.callback_query(F.data.startswith("group:set_proxy:"))
async def cb_group_set_proxy(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    # group:set_proxy:<group_id>:<proxy_id>
    parts = callback.data.split(":")
    group_id = int(parts[2])
    proxy_id = parts[3]

    group_repo = GroupRepository()
    err = await group_repo.check_proxy_assign_allowed(group_id, proxy_id)
    if err:
        await callback.answer(err, show_alert=True)
        return

    await group_repo.set_proxy(group_id, proxy_id)
    await callback.answer("✅ Прокси привязан к группе")
    callback.data = f"group:view:{group_id}"
    await cb_group_view(callback)


@router_comm.callback_query(F.data.startswith("group:clear_proxy:"))
async def cb_group_clear_proxy(callback: CallbackQuery) -> None:
    if not is_admin(callback.from_user.id):
        await callback.answer()
        return
    group_id = int(callback.data.split(":")[-1])
    await GroupRepository().set_proxy(group_id, None)
    await callback.answer("🔌 Прокси отвязан")
    callback.data = f"group:view:{group_id}"
    await cb_group_view(callback)

import asyncio
from functools import partial
from pathlib import Path

import gspread
from google.oauth2.service_account import Credentials

from config import settings
from utils.logger import logger
from utils.wa_auth import normalize_phone


def _sheets_enabled() -> bool:
    if not settings.SPREADSHEET_ID or not settings.SERVICE_ACCOUNT_PATH:
        return False
    return Path(settings.SERVICE_ACCOUNT_PATH).exists()


def _get_client() -> gspread.Client:
    creds = Credentials.from_service_account_file(
        settings.SERVICE_ACCOUNT_PATH, scopes=settings.SCOPES
    )
    return gspread.authorize(creds)


def _ensure_sheet(spreadsheet: gspread.Spreadsheet, title: str) -> gspread.Worksheet:
    try:
        return spreadsheet.worksheet(title)
    except gspread.WorksheetNotFound:
        return spreadsheet.add_worksheet(title=title, rows=1000, cols=20)


def _write_accounts_blocking(rows: list[list[str]]) -> None:
    gc = _get_client()
    ss = gc.open_by_key(settings.SPREADSHEET_ID)
    ws = _ensure_sheet(ss, settings.SHEET_ACCOUNTS)
    data = [settings.ACCOUNTS_HEADER] + rows
    ws.clear()
    ws.update(range_name="A1", values=data)
    ws.format("A1:Z1", {"textFormat": {"bold": True}})


def _write_communications_blocking(rows: list[list[str]]) -> None:
    gc = _get_client()
    ss = gc.open_by_key(settings.SPREADSHEET_ID)
    ws = _ensure_sheet(ss, settings.SHEET_COMMUNICATIONS)
    data = [settings.COMMUNICATIONS_HEADER] + rows
    ws.clear()
    ws.update(range_name="A1", values=data)
    ws.format("A1:Z1", {"textFormat": {"bold": True}})


async def sync_accounts() -> None:
    if not _sheets_enabled():
        return

    from utils.database import AccountRepository

    accounts = await AccountRepository().get_all()
    rows = [
        [
            str(a.id),
            normalize_phone(a.phone),
            a.jid or "",
            a.status,
            a.created_at.strftime("%d.%m.%Y %H:%M") if a.created_at else "",
        ]
        for a in accounts
    ]

    loop = asyncio.get_running_loop()
    retries = 3
    while retries:
        try:
            await loop.run_in_executor(None, partial(_write_accounts_blocking, rows))
            logger.info(
                f"Sheets: synced {len(rows)} accounts → «{settings.SHEET_ACCOUNTS}»"
            )
            break
        except Exception as e:
            retries -= 1
            logger.warning(
                f"Google Sheets accounts sync failed: {type(e).__name__}: {e}"
            )


async def sync_communications() -> None:
    """Write one report row per group into Communications sheet.

    Columns: comm_id, accounts, start_date, end_date, enabled, count_days, name
    accounts = comma-separated member account ids.
    Display-only — Go does not import this sheet for planning.
    """
    if not _sheets_enabled():
        return

    from utils.database import GroupRepository
    from utils.database.group_repo import STATUS_ENABLED, STATUS_FINISHED

    group_repo = GroupRepository()
    groups = await group_repo.get_all()
    rows: list[list[str]] = []

    for g in groups:
        if g.status not in (STATUS_ENABLED, STATUS_FINISHED):
            continue
        if not g.comm_id:
            continue
        members = await group_repo.get_members(g.id)
        account_ids = [m.id for m in members]
        if len(account_ids) < 2:
            continue

        start_date = ""
        if g.start_at:
            start_date = g.start_at.strftime("%Y-%m-%d")
        end_date = g.end_date or start_date
        enabled = "true" if g.status == STATUS_ENABLED else "false"
        count_days = str(g.days or 1)

        rows.append(
            [
                str(g.comm_id),
                ", ".join(str(aid) for aid in account_ids),
                start_date,
                end_date,
                enabled,
                count_days,
                g.name or "",
            ]
        )

    loop = asyncio.get_running_loop()
    retries = 3
    while retries:
        try:
            await loop.run_in_executor(
                None, partial(_write_communications_blocking, rows)
            )
            logger.info(
                f"Sheets: synced {len(rows)} communications → "
                f"«{settings.SHEET_COMMUNICATIONS}»"
            )
            break
        except Exception as e:
            retries -= 1
            logger.warning(
                f"Google Sheets communications sync failed: {type(e).__name__}: {e}"
            )

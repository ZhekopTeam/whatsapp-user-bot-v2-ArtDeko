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


async def sync_accounts() -> None:
    if not _sheets_enabled():
        return

    # Данные читаем в текущем event loop, в executor выносим только блокирующий вызов gspread.
    from utils.database import AccountRepository

    accounts = await AccountRepository().get_all()
    rows = [
        [
            str(a.id),
            normalize_phone(a.phone),
            a.jid or "",
            a.push_name or "",
            a.status,
            a.created_at.strftime("%d.%m.%Y %H:%M") if a.created_at else "",
        ]
        for a in accounts
    ]

    loop = asyncio.get_running_loop()
    try:
        await loop.run_in_executor(None, partial(_write_accounts_blocking, rows))
        logger.info(f"Sheets: synced {len(rows)} accounts")
    except Exception as e:
        logger.warning(f"Google Sheets sync failed: {type(e).__name__}: {e}")

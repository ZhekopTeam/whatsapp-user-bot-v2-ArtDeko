from utils.database import AccountRepository, GroupRepository
from utils.wa_auth import normalize_phone, remove_session


async def get_session_accounts() -> list[tuple[int, str, str]]:
    """Return (id, phone, status). Accounts in an active group get status 'warmup'."""
    repo = AccountRepository()
    accounts = await repo.get_all()
    busy = await GroupRepository().list_account_ids_in_active_groups()
    rows: list[tuple[int, str, str]] = []
    for a in accounts:
        if a.status == "revoked":
            status = "revoked"
        elif a.id in busy and a.status == "active":
            status = "warmup"
        else:
            status = a.status
        rows.append((a.id, a.phone, status))
    return rows


def account_status_label(status: str) -> str:
    return {
        "active": "активен",
        "warmup": "в прогреве",
        "revoked": "сессия слетела",
    }.get(status, status)


def mask_phone(phone: str) -> str:
    digits = phone.lstrip("+")
    if len(digits) < 4:
        return phone
    return f"+{digits[0]} *** *** {digits[-4:]}"


async def delete_session(phone: str) -> bool:
    repo = AccountRepository()
    account = await repo.get_by_phone(phone)
    if not account:
        return False

    await remove_session(normalize_phone(phone))
    await repo.delete_by_phone(phone)
    return True

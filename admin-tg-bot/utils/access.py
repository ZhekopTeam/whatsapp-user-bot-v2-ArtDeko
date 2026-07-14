"""Two-tier admin access: owners from .env ADMINS, runtime admins from DB."""

from __future__ import annotations

from config import settings

_db_admin_ids: set[int] = set()


def is_owner(tg_id: int) -> bool:
    return tg_id in settings.admins_list


def is_admin(tg_id: int) -> bool:
    return is_owner(tg_id) or tg_id in _db_admin_ids


def owner_ids() -> list[int]:
    return list(settings.admins_list)


def db_admin_ids() -> list[int]:
    return sorted(_db_admin_ids)


def all_admin_ids() -> list[int]:
    return sorted(set(settings.admins_list) | _db_admin_ids)


async def load_db_admins() -> None:
    """Reload in-memory DB admin set. Call after init_db() and after mutations."""
    global _db_admin_ids
    from utils.database.admin_repo import AdminRepository

    ids = await AdminRepository().list_ids()
    _db_admin_ids = set(ids)


async def add_admin(tg_id: int) -> None:
    from utils.database.admin_repo import AdminRepository

    await AdminRepository().add(tg_id)
    _db_admin_ids.add(tg_id)


async def remove_admin(tg_id: int) -> bool:
    """Remove a DB admin. Returns False if tg_id is an owner from .env."""
    if is_owner(tg_id):
        return False

    from utils.database.admin_repo import AdminRepository

    await AdminRepository().remove(tg_id)
    _db_admin_ids.discard(tg_id)
    return True

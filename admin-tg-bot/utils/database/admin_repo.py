from sqlalchemy import delete as sa_delete
from sqlalchemy import select

from .db_engine import get_session_factory
from .models import Admin


class AdminRepository:
    async def list_ids(self) -> list[int]:
        async with get_session_factory()() as session:
            result = await session.execute(select(Admin.tg_id))
            return list(result.scalars().all())

    async def add(self, tg_id: int) -> None:
        async with get_session_factory()() as session:
            existing = await session.get(Admin, tg_id)
            if existing is not None:
                return
            session.add(Admin(tg_id=tg_id))
            await session.commit()

    async def remove(self, tg_id: int) -> bool:
        async with get_session_factory()() as session:
            result = await session.execute(
                sa_delete(Admin).where(Admin.tg_id == tg_id)
            )
            await session.commit()
            return result.rowcount > 0

    async def exists(self, tg_id: int) -> bool:
        async with get_session_factory()() as session:
            return await session.get(Admin, tg_id) is not None

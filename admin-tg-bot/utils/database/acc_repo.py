from sqlalchemy import delete as sa_delete
from sqlalchemy import select
from sqlalchemy import update as sa_update

from .db_engine import get_session_factory
from .models import Account


class AccountRepository:
    async def add(self, account: Account) -> Account:
        async with get_session_factory()() as session:
            session.add(account)
            await session.commit()
            await session.refresh(account)
            return account

    async def set_proxy(self, account_id: int, proxy_id: str | None) -> None:
        async with get_session_factory()() as session:
            await session.execute(
                sa_update(Account)
                .where(Account.id == account_id)
                .values(proxy_id=proxy_id)
            )
            await session.commit()

    async def get_all(self) -> list[Account]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Account).order_by(Account.id)
            )
            return list(result.scalars().all())

    async def get_active(self) -> list[Account]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Account)
                .where(Account.status == "active")
                .order_by(Account.id)
            )
            return list(result.scalars().all())

    async def get_by_id(self, account_id: int) -> Account | None:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Account).where(Account.id == account_id)
            )
            return result.scalar_one_or_none()

    async def get_by_phone(self, phone: str) -> Account | None:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Account).where(Account.phone == phone)
            )
            return result.scalar_one_or_none()

    async def reactivate(
        self,
        phone: str,
        jid: str | None,
        admin_tg_id: int,
        proxy_id: str | None = None,
    ) -> None:
        async with get_session_factory()() as session:
            await session.execute(
                sa_update(Account)
                .where(Account.phone == phone)
                .values(
                    jid=jid,
                    status="active",
                    admin_tg_id=admin_tg_id,
                    proxy_id=proxy_id,
                )
            )
            await session.commit()

    async def mark_revoked(self, phone: str) -> None:
        async with get_session_factory()() as session:
            await session.execute(
                sa_update(Account)
                .where(Account.phone == phone)
                .values(status="revoked")
            )
            await session.commit()

    async def delete_by_phone(self, phone: str) -> bool:
        async with get_session_factory()() as session:
            result = await session.execute(
                sa_delete(Account).where(Account.phone == phone)
            )
            await session.commit()
            return result.rowcount > 0

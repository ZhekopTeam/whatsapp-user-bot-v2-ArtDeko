from uuid import uuid4

from sqlalchemy import delete as sa_delete
from sqlalchemy import select
from sqlalchemy import update as sa_update

from .db_engine import get_session_factory
from .models import Account, Proxy


class ProxyRepository:
    async def add(self, proxy: Proxy) -> Proxy:
        async with get_session_factory()() as session:
            session.add(proxy)
            await session.commit()
            await session.refresh(proxy)
            return proxy

    async def get_all(self) -> list[Proxy]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Proxy).order_by(Proxy.created_at)
            )
            return list(result.scalars().all())

    async def get_by_id(self, proxy_id: str) -> Proxy | None:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Proxy).where(Proxy.id == proxy_id)
            )
            return result.scalar_one_or_none()

    async def delete(self, proxy_id: str) -> bool:
        async with get_session_factory()() as session:
            result = await session.execute(
                sa_delete(Proxy).where(Proxy.id == proxy_id)
            )
            await session.commit()
            return result.rowcount > 0

    async def get_accounts_for_proxy(self, proxy_id: str) -> list[Account]:
        """Returns all accounts currently using this proxy."""
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Account).where(Account.proxy_id == proxy_id)
            )
            return list(result.scalars().all())

    async def assign_to_account(self, account_id: int, proxy_id: str | None) -> None:
        """Assign (or unassign if proxy_id is None) a proxy to an account."""
        async with get_session_factory()() as session:
            await session.execute(
                sa_update(Account)
                .where(Account.id == account_id)
                .values(proxy_id=proxy_id)
            )
            await session.commit()

    async def check_assign_allowed(
        self, account_id: int, proxy_id: str
    ) -> str | None:
        """Validate group/proxy exclusivity. Returns error text or None if OK."""
        from .group_repo import GroupRepository

        group_repo = GroupRepository()
        target_group = await group_repo.get_group_id_for_account(account_id)

        # Proxy already used by another group?
        proxy_groups = await group_repo.get_group_ids_for_proxy(proxy_id)
        if target_group is not None:
            other = proxy_groups - {target_group}
        else:
            other = proxy_groups
        if other:
            return (
                "Этот прокси уже используется другой группой аккаунтов.\n"
                "Один прокси нельзя подключать к нескольким группам."
            )

        # If account is in a group, all members must share the same proxy
        if target_group is not None:
            members = await group_repo.get_members(target_group)
            for m in members:
                if m.id == account_id:
                    continue
                if m.proxy_id is not None and m.proxy_id != proxy_id:
                    return (
                        "В группе уже другой прокси.\n"
                        "Все аккаунты группы должны использовать один прокси."
                    )

        accounts_on_proxy = await self.get_accounts_for_proxy(proxy_id)
        if len(accounts_on_proxy) >= 6 and not any(
            a.id == account_id for a in accounts_on_proxy
        ):
            return "Прокси уже заполнен (максимум 6 аккаунтов)."

        return None

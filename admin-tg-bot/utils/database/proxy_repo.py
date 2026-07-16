from sqlalchemy import delete as sa_delete
from sqlalchemy import select

from .db_engine import get_session_factory
from .models import Account, AccountGroup, AccountGroupMember, Proxy


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

    async def get_account_for_proxy(self, proxy_id: str) -> Account | None:
        """The account this proxy is bound to (proxy is one-per-account now)."""
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Account).where(Account.proxy_id == proxy_id)
            )
            return result.scalars().first()

    async def list_free(self) -> list[Proxy]:
        """Proxies not yet bound to any account."""
        async with get_session_factory()() as session:
            taken = select(Account.proxy_id).where(Account.proxy_id.is_not(None))
            result = await session.execute(
                select(Proxy)
                .where(Proxy.id.not_in(taken))
                .order_by(Proxy.created_at)
            )
            return list(result.scalars().all())

    async def get_group_for_proxy(self, proxy_id: str) -> AccountGroup | None:
        from .group_repo import STATUS_ENABLED

        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroup).where(
                    AccountGroup.proxy_id == proxy_id,
                    AccountGroup.status == STATUS_ENABLED,
                )
            )
            return result.scalar_one_or_none()

    async def get_accounts_via_group(self, proxy_id: str) -> list[Account]:
        """Accounts that inherit this proxy through their active group."""
        from .group_repo import STATUS_ENABLED

        async with get_session_factory()() as session:
            result = await session.execute(
                select(Account)
                .join(
                    AccountGroupMember,
                    AccountGroupMember.account_id == Account.id,
                )
                .join(
                    AccountGroup,
                    AccountGroup.id == AccountGroupMember.group_id,
                )
                .where(
                    AccountGroup.proxy_id == proxy_id,
                    AccountGroup.status == STATUS_ENABLED,
                )
                .order_by(AccountGroupMember.position)
            )
            return list(result.scalars().all())

from datetime import datetime

from sqlalchemy import delete as sa_delete
from sqlalchemy import select
from sqlalchemy import update as sa_update

from .db_engine import get_session_factory
from .models import Account, AccountGroup, AccountGroupMember


MAX_GROUP_SIZE = 6
STATUS_ENABLED = "enabled"
STATUS_FINISHED = "finished"


class GroupRepository:
    async def create(
        self,
        account_ids: list[int],
        *,
        name: str,
        proxy_id: str | None = None,
        start_at: datetime | None = None,
        days: int = 1,
        end_date: str | None = None,
        comm_id: int | None = None,
        status: str = STATUS_ENABLED,
    ) -> AccountGroup:
        if not 2 <= len(account_ids) <= MAX_GROUP_SIZE:
            raise ValueError(
                f"Group must have 2–{MAX_GROUP_SIZE} accounts, got {len(account_ids)}"
            )
        if len(set(account_ids)) != len(account_ids):
            raise ValueError("Duplicate accounts in group")

        async with get_session_factory()() as session:
            group = AccountGroup(
                name=name.strip() or "Группа",
                status=status,
                proxy_id=proxy_id,
                start_at=start_at,
                days=days,
                end_date=end_date,
                comm_id=comm_id,
            )
            session.add(group)
            await session.flush()
            for pos, acc_id in enumerate(account_ids, start=1):
                session.add(
                    AccountGroupMember(
                        group_id=group.id,
                        account_id=acc_id,
                        position=pos,
                    )
                )
            await session.commit()
            await session.refresh(group)
            return group

    async def get_all(self) -> list[AccountGroup]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroup).order_by(AccountGroup.id.desc())
            )
            return list(result.scalars().all())

    async def get_by_status(self, status: str) -> list[AccountGroup]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroup)
                .where(AccountGroup.status == status)
                .order_by(AccountGroup.id.desc())
            )
            return list(result.scalars().all())

    async def get_by_id(self, group_id: int) -> AccountGroup | None:
        async with get_session_factory()() as session:
            return await session.get(AccountGroup, group_id)

    async def delete(self, group_id: int) -> bool:
        async with get_session_factory()() as session:
            result = await session.execute(
                sa_delete(AccountGroup).where(AccountGroup.id == group_id)
            )
            await session.commit()
            return result.rowcount > 0

    async def set_status(self, group_id: int, status: str) -> None:
        async with get_session_factory()() as session:
            await session.execute(
                sa_update(AccountGroup)
                .where(AccountGroup.id == group_id)
                .values(status=status)
            )
            await session.commit()

    async def set_comm_id(self, group_id: int, comm_id: int) -> None:
        async with get_session_factory()() as session:
            await session.execute(
                sa_update(AccountGroup)
                .where(AccountGroup.id == group_id)
                .values(comm_id=comm_id)
            )
            await session.commit()

    async def get_members(self, group_id: int) -> list[Account]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(Account)
                .join(
                    AccountGroupMember,
                    AccountGroupMember.account_id == Account.id,
                )
                .where(AccountGroupMember.group_id == group_id)
                .order_by(AccountGroupMember.position)
            )
            return list(result.scalars().all())

    async def get_group_id_for_account(self, account_id: int) -> int | None:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroupMember.group_id)
                .join(AccountGroup, AccountGroup.id == AccountGroupMember.group_id)
                .where(
                    AccountGroupMember.account_id == account_id,
                    AccountGroup.status == STATUS_ENABLED,
                )
            )
            return result.scalar_one_or_none()

    async def list_account_ids_in_active_groups(self) -> set[int]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroupMember.account_id)
                .join(AccountGroup, AccountGroup.id == AccountGroupMember.group_id)
                .where(AccountGroup.status == STATUS_ENABLED)
            )
            return set(result.scalars().all())

    async def list_account_ids_in_groups(self) -> set[int]:
        """Backward-compatible alias: only active (enabled) groups."""
        return await self.list_account_ids_in_active_groups()

    async def get_group_id_for_proxy(self, proxy_id: str) -> int | None:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroup.id).where(
                    AccountGroup.proxy_id == proxy_id,
                    AccountGroup.status == STATUS_ENABLED,
                )
            )
            return result.scalar_one_or_none()

    async def list_used_proxy_ids(self) -> set[str]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroup.proxy_id).where(
                    AccountGroup.proxy_id.is_not(None),
                    AccountGroup.status == STATUS_ENABLED,
                )
            )
            return {pid for pid in result.scalars().all() if pid}

    async def set_proxy(self, group_id: int, proxy_id: str | None) -> None:
        async with get_session_factory()() as session:
            await session.execute(
                sa_update(AccountGroup)
                .where(AccountGroup.id == group_id)
                .values(proxy_id=proxy_id)
            )
            await session.commit()

    async def check_proxy_assign_allowed(
        self,
        group_id: int,
        proxy_id: str,
    ) -> str | None:
        owner = await self.get_group_id_for_proxy(proxy_id)
        if owner is not None and owner != group_id:
            return (
                "Этот прокси уже привязан к другой группе.\n"
                "Один прокси — только одна группа."
            )
        return None

    async def auto_finish_expired(self, today: str) -> int:
        """Mark enabled groups as finished when end_date < today. Returns count."""
        async with get_session_factory()() as session:
            result = await session.execute(
                sa_update(AccountGroup)
                .where(
                    AccountGroup.status == STATUS_ENABLED,
                    AccountGroup.end_date.is_not(None),
                    AccountGroup.end_date < today,
                )
                .values(status=STATUS_FINISHED)
            )
            await session.commit()
            return result.rowcount or 0

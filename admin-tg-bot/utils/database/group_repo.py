from sqlalchemy import delete as sa_delete
from sqlalchemy import select

from .db_engine import get_session_factory
from .models import Account, AccountGroup, AccountGroupMember


MAX_GROUP_SIZE = 6


class GroupRepository:
    async def create(self, account_ids: list[int]) -> AccountGroup:
        if not 2 <= len(account_ids) <= MAX_GROUP_SIZE:
            raise ValueError(
                f"Group must have 2–{MAX_GROUP_SIZE} accounts, got {len(account_ids)}"
            )
        if len(set(account_ids)) != len(account_ids):
            raise ValueError("Duplicate accounts in group")

        async with get_session_factory()() as session:
            group = AccountGroup()
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
                select(AccountGroup).order_by(AccountGroup.id)
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
                select(AccountGroupMember.group_id).where(
                    AccountGroupMember.account_id == account_id
                )
            )
            return result.scalar_one_or_none()

    async def list_account_ids_in_groups(self) -> set[int]:
        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroupMember.account_id)
            )
            return set(result.scalars().all())

    async def get_group_ids_for_proxy(self, proxy_id: str) -> set[int]:
        """Groups that have at least one account using this proxy."""
        async with get_session_factory()() as session:
            result = await session.execute(
                select(AccountGroupMember.group_id)
                .join(Account, Account.id == AccountGroupMember.account_id)
                .where(Account.proxy_id == proxy_id)
            )
            return set(result.scalars().all())

from pathlib import Path

from sqlalchemy import event, text
from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    async_sessionmaker,
    create_async_engine,
)

from config import settings
from .models import Base

_engine: AsyncEngine | None = None
_session_factory: async_sessionmaker[AsyncSession] | None = None


def get_session_factory() -> async_sessionmaker[AsyncSession]:
    if _session_factory is None:
        raise RuntimeError(
            "Database not initialized. Call init_db() at startup.")
    return _session_factory


async def init_db() -> None:
    global _engine, _session_factory

    db_path = Path(settings.DATABASE_PATH)
    db_path.parent.mkdir(parents=True, exist_ok=True)

    _engine = create_async_engine(
        f"sqlite+aiosqlite:///{db_path}",
        echo=settings.DB_ECHO,
    )

    @event.listens_for(_engine.sync_engine, "connect")
    def _set_sqlite_pragma(dbapi_conn, _):  # noqa: ANN001
        cursor = dbapi_conn.cursor()
        cursor.execute("PRAGMA foreign_keys=ON")
        cursor.close()

    _session_factory = async_sessionmaker(_engine, expire_on_commit=False)

    async with _engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

        def _migrate_db(connection):
            res = connection.execute(text("PRAGMA table_info(accounts)"))
            columns = [row[1] for row in res.fetchall()]
            if "proxy_id" not in columns:
                connection.execute(
                    text(
                        "ALTER TABLE accounts ADD COLUMN proxy_id VARCHAR(36) REFERENCES proxies(id) ON DELETE SET NULL"
                    )
                )

            res = connection.execute(text("PRAGMA table_info(account_groups)"))
            group_columns = [row[1] for row in res.fetchall()]
            if group_columns and "proxy_id" not in group_columns:
                connection.execute(
                    text(
                        "ALTER TABLE account_groups ADD COLUMN proxy_id VARCHAR(36) REFERENCES proxies(id) ON DELETE SET NULL"
                    )
                )
            migrations = {
                "name": "ALTER TABLE account_groups ADD COLUMN name VARCHAR(128) NOT NULL DEFAULT ''",
                "status": "ALTER TABLE account_groups ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'enabled'",
                "start_at": "ALTER TABLE account_groups ADD COLUMN start_at DATETIME",
                "days": "ALTER TABLE account_groups ADD COLUMN days INTEGER NOT NULL DEFAULT 1",
                "end_date": "ALTER TABLE account_groups ADD COLUMN end_date VARCHAR(10)",
                "comm_id": "ALTER TABLE account_groups ADD COLUMN comm_id INTEGER",
            }
            for col, ddl in migrations.items():
                if group_columns and col not in group_columns:
                    connection.execute(text(ddl))

        await conn.run_sync(_migrate_db)

from datetime import datetime, timezone

from sqlalchemy import BigInteger, DateTime, Integer, String
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    pass


class Account(Base):
    __tablename__ = "accounts"

    # account_id целочисленный и совместим с Go-ботом (читает account_id из Google Sheets как int).
    id: Mapped[int] = mapped_column(
        Integer, primary_key=True, autoincrement=True)
    phone: Mapped[str] = mapped_column(String(20), unique=True, nullable=False)
    jid: Mapped[str | None] = mapped_column(
        String(64), unique=True, nullable=True)
    push_name: Mapped[str | None] = mapped_column(String(128), nullable=True)
    admin_tg_id: Mapped[int | None] = mapped_column(BigInteger, nullable=True)
    status: Mapped[str] = mapped_column(
        String(16), default="active", nullable=False
    )
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=lambda: datetime.now(timezone.utc),
    )

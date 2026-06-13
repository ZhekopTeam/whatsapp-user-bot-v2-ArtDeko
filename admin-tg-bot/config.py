import json
import os
from pathlib import Path
from typing import Optional

from aiogram import Bot, Dispatcher
from aiogram.client.default import DefaultBotProperties
from dotenv import load_dotenv
from pydantic import Field, field_validator
from pydantic_settings import BaseSettings

load_dotenv()


class Settings(BaseSettings):
    LOG_LEVEL: str = Field(os.getenv("LOG_LEVEL", "INFO"))
    LOG_FILE: Optional[str] = Field(os.getenv("LOG_FILE", None))
    LOG_FORMAT: str = Field(
        os.getenv("LOG_FORMAT", "%(levelname)-8s | %(asctime)s | %(message)s")
    )
    LOG_DATE_FORMAT: str = Field(
        os.getenv("LOG_DATE_FORMAT", "%H:%M:%S %d-%m-%Y"))

    BOT_TOKEN: Optional[str] = Field(os.getenv("BOT_TOKEN", None))
    ADMINS: str = Field(os.getenv("ADMINS", ""))

    DB_ECHO: bool = Field(os.getenv("DB_ECHO", "false").lower() == "true")
    DATABASE_PATH: str = Field(
        os.getenv("DATABASE_PATH", "data/wa_bot_accounts.db")
    )

    # Путь к рантайм-хранилищу Go-бота прогрева (база данных с задачами и логами)
    RUNTIME_DB_PATH: str = Field(
        os.getenv("RUNTIME_DB_PATH", "sessions/runtime.db")
    )

    # Путь к whatsmeow-хранилищу. Должен совпадать с SESSION_DB_PATH Go-бота прогрева,
    # так как QR-авторизация пишет сессию именно туда.
    SESSION_DB_PATH: str = Field(
        os.getenv("SESSION_DB_PATH", "file:sessions/multi.db?_foreign_keys=on")
    )
    # Путь к Go-бинарнику WhatsApp-бота, который выполняет QR-авторизацию.
    WHATSAPP_BIN: str = Field(os.getenv("WHATSAPP_BIN", "/app/wh-user-bot"))
    WHATSAPP_API_URL: str = Field(
        os.getenv("WHATSAPP_API_URL", "http://bot:5001")
    )
    AUTH_TIMEOUT_SEC: int = Field(int(os.getenv("AUTH_TIMEOUT_SEC", "180")))

    SPREADSHEET_ID: str = Field(os.getenv("SPREADSHEET_ID", ""))
    SERVICE_ACCOUNT_PATH: str = Field(
        os.getenv("SERVICE_ACCOUNT_PATH", "data/service_account.json")
    )
    SCOPES: list[str] = Field(
        os.getenv("SCOPES", '["https://www.googleapis.com/auth/spreadsheets"]')
    )
    SHEET_ACCOUNTS: str = Field(
        os.getenv("SHEET_ACCOUNTS", "WhatsApp Accounts"))
    # Колонки A:B (account_id, ph_number) читает Go-бот; остальные — для наглядности.
    ACCOUNTS_HEADER: list[str] = Field(
        os.getenv(
            "ACCOUNTS_HEADER",
            '["account_id", "ph_number", "jid", "status", "created_at"]',
        )
    )

    @field_validator("SCOPES", "ACCOUNTS_HEADER", mode="before")
    @classmethod
    def _parse_list(cls, v: object) -> object:
        if isinstance(v, str):
            return json.loads(v)
        return v

    @property
    def admins_list(self) -> list[int]:
        return [int(i.strip()) for i in self.ADMINS.split(",") if i.strip()]


settings = Settings()

bot: Optional[Bot] = None
dp: Optional[Dispatcher] = None

if settings.BOT_TOKEN:
    bot = Bot(
        settings.BOT_TOKEN,
        default=DefaultBotProperties(parse_mode="HTML"),
    )
    dp = Dispatcher()

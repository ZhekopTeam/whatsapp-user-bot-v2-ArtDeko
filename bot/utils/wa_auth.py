import asyncio
import json
import os
from io import BytesIO
from typing import AsyncIterator, Optional

import qrcode

from config import settings
from utils.logger import logger


def normalize_phone(phone: str) -> str:
    return "".join(ch for ch in phone if ch.isdigit())


def is_valid_phone(phone: str) -> bool:
    digits = normalize_phone(phone)
    return 8 <= len(digits) <= 15


def render_qr_png(code: str) -> bytes:
    qr = qrcode.QRCode(
        error_correction=qrcode.constants.ERROR_CORRECT_L,
        box_size=8,
        border=2,
    )
    qr.add_data(code)
    qr.make(fit=True)
    img = qr.make_image(fill_color="black", back_color="white")
    buf = BytesIO()
    img.save(buf, format="PNG")
    return buf.getvalue()


class WhatsAppAuth:
    """Мост к Go-бинарнику: запускает `auth-qr`, читает NDJSON-события, отдаёт их потоком."""

    def __init__(self) -> None:
        self._proc: Optional[asyncio.subprocess.Process] = None

    @property
    def busy(self) -> bool:
        return self._proc is not None

    async def stream(self, admin_tg_id: int, phone: str) -> AsyncIterator[dict]:
        if admin_tg_id not in settings.admins_list:
            raise PermissionError("User is not admin")
        if self.busy:
            raise RuntimeError(
                "Авторизация уже выполняется, дождитесь её завершения")

        env = os.environ.copy()
        env["SESSION_DB_PATH"] = settings.SESSION_DB_PATH

        proc = await asyncio.create_subprocess_exec(
            settings.WHATSAPP_BIN,
            "auth-qr",
            "--phone",
            phone,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env=env,
        )
        self._proc = proc
        logger.info(f"auth-qr started for {phone} (admin={admin_tg_id})")

        try:
            while True:
                try:
                    raw = await asyncio.wait_for(
                        proc.stdout.readline(), timeout=settings.AUTH_TIMEOUT_SEC
                    )
                except asyncio.TimeoutError:
                    logger.warning("auth-qr timed out, terminating")
                    yield {"event": "timeout", "message": "Время авторизации истекло"}
                    break

                if not raw:
                    break
                line = raw.strip()
                if not line:
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    logger.warning(f"auth-qr non-JSON output: {line!r}")
                    continue
                yield event

            await proc.wait()
            if proc.returncode not in (0, None):
                err = (await proc.stderr.read()).decode(errors="replace").strip()
                if err:
                    logger.warning(f"auth-qr stderr: {err}")
        finally:
            await self._terminate()

    async def cancel(self) -> None:
        await self._terminate()

    async def _terminate(self) -> None:
        proc = self._proc
        self._proc = None
        if proc is None or proc.returncode is not None:
            return
        try:
            proc.terminate()
            await asyncio.wait_for(proc.wait(), timeout=5)
        except asyncio.TimeoutError:
            try:
                proc.kill()
            except ProcessLookupError:
                pass
        except ProcessLookupError:
            pass


async def remove_session(phone: str) -> bool:
    """Удаляет WhatsApp-сессию из multi.db через Go-бинарник. Возвращает True при удалении."""
    env = os.environ.copy()
    env["SESSION_DB_PATH"] = settings.SESSION_DB_PATH
    try:
        proc = await asyncio.create_subprocess_exec(
            settings.WHATSAPP_BIN,
            "logout-wa",
            "--phone",
            phone,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env=env,
        )
        stdout, stderr = await proc.communicate()
    except FileNotFoundError:
        logger.error(f"WhatsApp binary not found: {settings.WHATSAPP_BIN}")
        return False

    if proc.returncode != 0:
        logger.warning(
            f"logout-wa failed: {stderr.decode(errors='replace').strip()}")
        return False

    for line in stdout.decode(errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            data = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(data, dict) and "removed" in data:
            return bool(data["removed"])
    return False

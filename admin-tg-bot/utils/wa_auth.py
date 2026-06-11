import asyncio
import json
import os
from io import BytesIO
from typing import AsyncIterator, Optional

import aiohttp
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
        error_correction=qrcode.constants.ERROR_CORRECT_H,
        box_size=8,
        border=2,
    )
    qr.add_data(code)
    qr.make(fit=True)
    img = qr.make_image(fill_color="black", back_color="white")
    buf = BytesIO()
    img.save(buf)
    
    return buf.getvalue()



class WhatsAppAuth:
    """Мост к Go-демону: делает HTTP-запрос к API, читает NDJSON-события, отдаёт их потоком."""

    def __init__(self) -> None:
        self._session: Optional[aiohttp.ClientSession] = None
        self._response: Optional[aiohttp.ClientResponse] = None

    @property
    def busy(self) -> bool:
        return self._session is not None

    async def stream(self, admin_tg_id: int, phone: str) -> AsyncIterator[dict]:
        if admin_tg_id not in settings.admins_list:
            raise PermissionError("User is not admin")
        if self.busy:
            raise RuntimeError(
                "Авторизация уже выполняется, дождитесь её завершения")

        self._session = aiohttp.ClientSession()
        logger.info(f"HTTP auth stream starting for {phone} (admin={admin_tg_id})")

        try:
            url = f"{settings.WHATSAPP_API_URL}/auth/qr"
            params = {"phone": phone}
            
            self._response = await self._session.get(
                url, params=params, timeout=settings.AUTH_TIMEOUT_SEC
            )
            
            if self._response.status != 200:
                err_text = await self._response.text()
                yield {"event": "error", "message": f"HTTP {self._response.status}: {err_text}"}
                return

            async for line in self._response.content:
                line = line.strip()
                if not line:
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    logger.warning(f"HTTP auth non-JSON line: {line!r}")
                    continue
                yield event

        except asyncio.TimeoutError:
            logger.warning("HTTP auth stream timed out")
            yield {"event": "timeout", "message": "Время авторизации истекло"}
        except aiohttp.ClientError as e:
            logger.warning(f"HTTP auth ClientError: {e}")
            yield {"event": "error", "message": f"Ошибка соединения: {e}"}
        finally:
            await self._close()

    async def cancel(self) -> None:
        await self._close()

    async def _close(self) -> None:
        if self._response:
            self._response.close()
            self._response = None
        if self._session:
            await self._session.close()
            self._session = None


async def remove_session(phone: str) -> bool:
    """Удаляет WhatsApp-сессию через HTTP-запрос к Go-демону. Возвращает True при удалении."""
    url = f"{settings.WHATSAPP_API_URL}/logout"
    params = {"phone": phone}
    try:
        async with aiohttp.ClientSession() as session:
            async with session.post(url, params=params) as response:
                if response.status != 200:
                    err_text = await response.text()
                    logger.warning(f"logout request failed: HTTP {response.status}: {err_text}")
                    return False
                data = await response.json()
                if isinstance(data, dict) and "removed" in data:
                    return bool(data["removed"])
    except aiohttp.ClientError as e:
        logger.error(f"Failed to communicate with Go API for logout: {e}")
        return False
    return False

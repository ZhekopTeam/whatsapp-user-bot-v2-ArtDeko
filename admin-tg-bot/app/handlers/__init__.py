from aiogram import Router

from .accounts_auth import router_auth
from .accounts_info import router_info

router_handlers = Router()
router_handlers.include_routers(
    router_info,
    router_auth,
)

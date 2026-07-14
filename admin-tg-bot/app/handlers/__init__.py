from aiogram import Router

from .accounts_auth import router_auth
from .accounts_info import router_info
from .admins_handlers import router_admins
from .communications import router_comm
from .proxy_handlers import router_proxy

router_handlers = Router()
router_handlers.include_routers(
    router_info,
    router_auth,
    router_comm,
    router_proxy,
    router_admins,
)

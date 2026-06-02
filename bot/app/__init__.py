from aiogram import Router

from utils import CommandPriorityMiddleware
from .handlers import router_handlers

router_main = Router()
router_main.message.middleware(CommandPriorityMiddleware())
router_main.include_routers(router_handlers)

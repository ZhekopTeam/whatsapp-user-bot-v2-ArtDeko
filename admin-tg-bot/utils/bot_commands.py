from aiogram import Bot
from aiogram.types import BotCommand, BotCommandScopeDefault


async def set_command(bot: Bot) -> None:
    commands = [
        BotCommand(command="start", description="старт"),
    ]
    await bot.set_my_commands(commands, scope=BotCommandScopeDefault())

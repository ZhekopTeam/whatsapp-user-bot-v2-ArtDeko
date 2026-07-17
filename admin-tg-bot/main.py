import asyncio

import app
from config import bot, dp, settings
from utils import WhatsAppAuth, set_command
from utils.access import all_admin_ids, load_db_admins
from utils.database import init_db
from utils.logger import logger


async def _wait_for_telegram() -> None:
    delay = 5
    for attempt in range(1, 13):
        try:
            me = await bot.get_me()
            logger.info(f"me - @{me.username}")
            return
        except Exception as e:
            logger.warning(
                f"Telegram unavailable (attempt {attempt}/12): {e}. Retry in {delay}s"
            )
            await asyncio.sleep(delay)
            delay = min(delay * 2, 60)
    raise RuntimeError("Telegram is unreachable after 12 attempts, giving up")


async def main() -> None:
    if bot is None or dp is None:
        raise RuntimeError("BOT_TOKEN is not set in .env")

    await _wait_for_telegram()
    logger.info(f"owners: {settings.admins_list}")

    await init_db()
    await load_db_admins()
    logger.info(f"all admins: {all_admin_ids()}")

    from utils.sheets_sync import sync_accounts, sync_communications

    asyncio.create_task(sync_accounts())
    asyncio.create_task(sync_communications())

    wa_auth = WhatsAppAuth()

    dp.include_router(app.router_main)
    await set_command(bot)
    await bot.delete_webhook(drop_pending_updates=True)

    try:
        await dp.start_polling(
            bot,
            skip_updates=True,
            wa_auth=wa_auth,
        )
    except Exception as e:
        logger.error(f"Fatal error in polling loop: {e}")
        raise
    finally:
        await wa_auth.cancel()


if __name__ == "__main__":
    asyncio.run(main())

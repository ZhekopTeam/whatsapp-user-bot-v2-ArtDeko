from aiogram.fsm.state import State, StatesGroup


class AddAccount(StatesGroup):
    waiting_phone = State()
    waiting_qr = State()


class CreateChain(StatesGroup):
    choosing_accounts = State()
    choosing_start_time = State()
    waiting_custom_time = State()


class AddProxy(StatesGroup):
    waiting_proxy_input = State()
    waiting_proxy_name = State()


class AddAdmin(StatesGroup):
    waiting_tg_id = State()

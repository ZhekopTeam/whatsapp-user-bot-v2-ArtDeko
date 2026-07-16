from aiogram.fsm.state import State, StatesGroup


class AddAccount(StatesGroup):
    waiting_phone = State()
    waiting_qr = State()


class CreateGroup(StatesGroup):
    waiting_name = State()
    choosing_accounts = State()
    choosing_start_time = State()
    waiting_custom_time = State()
    choosing_days = State()
    waiting_custom_days = State()
    choosing_proxy = State()
    confirming = State()


class AddProxy(StatesGroup):
    waiting_proxy_input = State()
    waiting_proxy_name = State()


class AddAdmin(StatesGroup):
    waiting_tg_id = State()

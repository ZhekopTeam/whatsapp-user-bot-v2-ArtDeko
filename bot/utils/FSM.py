from aiogram.fsm.state import State, StatesGroup


class AddAccount(StatesGroup):
    waiting_phone = State()
    waiting_qr = State()

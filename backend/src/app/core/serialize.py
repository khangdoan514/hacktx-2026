from datetime import date, datetime
from decimal import Decimal
from uuid import UUID


def dump(value):
    if isinstance(value, dict):
        return {key: dump(item) for key, item in value.items()}
    if isinstance(value, list):
        return [dump(item) for item in value]
    if isinstance(value, tuple):
        return [dump(item) for item in value]
    if isinstance(value, (datetime, date)):
        return value.isoformat()
    if isinstance(value, UUID):
        return str(value)
    if isinstance(value, Decimal):
        return format(value, "f")
    return value

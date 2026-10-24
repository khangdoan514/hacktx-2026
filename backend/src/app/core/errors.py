from contextvars import ContextVar

request_id_var: ContextVar[str] = ContextVar("request_id", default="")


class AppError(Exception):
    def __init__(
        self,
        status: int,
        code: str,
        message: str,
        fields: dict | None = None,
    ) -> None:
        self.status = status
        self.code = code
        self.message = message
        self.fields = fields or {}
        super().__init__(message)

    def body(self) -> dict:
        return {
            "code": self.code,
            "message": self.message,
            "fields": self.fields,
            "request_id": request_id_var.get(),
        }

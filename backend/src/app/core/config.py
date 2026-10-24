from pydantic import model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    app_name: str = "HackTX 2026 API"
    environment: str = "production"
    cors_origins: str = "http://localhost:3000"
    database_url: str = ""
    jwt_secret: str = "dev-only-change-me"
    jwt_issuer: str = "http://127.0.0.1/auth/v1"
    jwt_audience: str = "authenticated"
    allow_local_auth: bool = False
    storage_dir: str = "var/storage"
    convex_url: str = ""
    convex_http_url: str = ""
    convex_hint_secret: str = ""
    supabase_url: str = ""

    @model_validator(mode="after")
    def local_auth_is_not_production(self) -> "Settings":
        if self.allow_local_auth and self.environment not in {"local", "test"}:
            raise ValueError("ALLOW_LOCAL_AUTH requires ENVIRONMENT=local or ENVIRONMENT=test")
        return self

    @property
    def cors_origin_list(self) -> list[str]:
        return [origin.strip() for origin in self.cors_origins.split(",") if origin.strip()]


settings = Settings()

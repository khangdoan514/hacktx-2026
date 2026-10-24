from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    app_name: str = "HackTX 2026 API"
    cors_origins: str = "http://localhost:3000"
    database_url: str = ""
    jwt_secret: str = "dev-only-change-me"
    jwt_issuer: str = "http://127.0.0.1/auth/v1"
    jwt_audience: str = "authenticated"
    allow_local_auth: bool = False
    storage_dir: str = "var/storage"
    convex_url: str = ""
    convex_deploy_key: str = ""
    supabase_url: str = ""

    @property
    def cors_origin_list(self) -> list[str]:
        return [origin.strip() for origin in self.cors_origins.split(",") if origin.strip()]


settings = Settings()

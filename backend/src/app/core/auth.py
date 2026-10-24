import hashlib
import hmac
import secrets
from datetime import datetime, timedelta, timezone
from uuid import UUID, uuid4

import jwt
from fastapi import Header
from pydantic import BaseModel, Field

from app.core.config import settings
from app.core.database import admin_connection, user_connection
from app.core.errors import AppError
from app.core.serialize import dump


class Actor(BaseModel):
    user_id: str
    email: str


class LocalAuthIn(BaseModel):
    email: str = Field(min_length=3, max_length=320)
    password: str = Field(min_length=8, max_length=200)
    display_name: str | None = Field(default=None, max_length=120)
    create: bool = False


def hash_password(password: str, salt: str | None = None) -> str:
    salt = salt or secrets.token_hex(16)
    digest = hashlib.scrypt(
        password.encode(),
        salt=salt.encode(),
        n=2**14,
        r=8,
        p=1,
        dklen=32,
    ).hex()
    return f"scrypt${salt}${digest}"


def verify_password(password: str, stored: str) -> bool:
    parts = stored.split("$")
    if len(parts) != 3 or parts[0] != "scrypt":
        return False
    _, salt, digest = parts
    check = hash_password(password, salt).split("$", 2)[2]
    return hmac.compare_digest(check, digest)


def encode_token(user_id: str, email: str) -> str:
    now = datetime.now(timezone.utc)
    payload = {
        "sub": str(user_id),
        "email": email,
        "aud": settings.jwt_audience,
        "iss": settings.jwt_issuer,
        "iat": now,
        "exp": now + timedelta(hours=12),
    }
    return jwt.encode(payload, settings.jwt_secret, algorithm="HS256")


def decode_token(token: str) -> dict:
    try:
        return jwt.decode(
            token,
            settings.jwt_secret,
            algorithms=["HS256"],
            audience=settings.jwt_audience,
            issuer=settings.jwt_issuer,
        )
    except jwt.PyJWTError as exc:
        raise AppError(401, "unauthenticated", "Invalid or expired session") from exc


def get_actor(authorization: str | None = Header(default=None)) -> Actor:
    if not authorization or not authorization.lower().startswith("bearer "):
        raise AppError(401, "unauthenticated", "Authentication required")
    payload = decode_token(authorization.split(" ", 1)[1].strip())
    user_id = str(payload.get("sub") or "")
    email = str(payload.get("email") or "")
    try:
        UUID(user_id)
    except ValueError as exc:
        raise AppError(401, "unauthenticated", "Invalid or expired session") from exc
    with user_connection(user_id, email) as conn:
        profile = conn.execute(
            "SELECT id, status FROM app.profiles WHERE id = %s",
            (user_id,),
        ).fetchone()
    if not profile or profile["status"] != "active":
        raise AppError(401, "unauthenticated", "Unknown account")
    return Actor(user_id=user_id, email=email)


def local_session(body: LocalAuthIn) -> dict:
    if not settings.allow_local_auth:
        raise AppError(404, "not_found", "Not found")
    email = body.email.strip().lower()
    if "@" not in email or email.startswith("@") or email.endswith("@"):
        raise AppError(422, "validation", "Enter a valid email address", {"email": "invalid"})
    with admin_connection() as conn:
        existing = conn.execute(
            "SELECT * FROM app.lookup_local_credential(%s)",
            (email,),
        ).fetchone()
        if existing:
            if body.create:
                raise AppError(409, "account_exists", "An account with that email already exists")
            if not verify_password(body.password, existing["password_hash"]):
                raise AppError(401, "invalid_credentials", "Email or password is incorrect")
            user_id = existing["user_id"]
        else:
            if not body.create:
                raise AppError(401, "invalid_credentials", "Email or password is incorrect")
            user_id = uuid4()
            display = (body.display_name or email.split("@")[0]).strip()
            conn.execute(
                "INSERT INTO auth.users (id, email) VALUES (%s, %s)",
                (user_id, email),
            )
            conn.execute(
                """
                INSERT INTO app.profiles (id, display_name, status, timezone)
                VALUES (%s, %s, 'active', 'UTC')
                """,
                (user_id, display),
            )
            conn.execute(
                """
                INSERT INTO app.local_credentials (user_id, email, password_hash)
                VALUES (%s, %s, %s)
                """,
                (user_id, email, hash_password(body.password)),
            )
        profile = conn.execute(
            "SELECT id, display_name, status, timezone FROM app.profiles WHERE id = %s",
            (user_id,),
        ).fetchone()
    return {
        "access_token": encode_token(str(user_id), email),
        "token_type": "bearer",
        "profile": dump(dict(profile)),
    }

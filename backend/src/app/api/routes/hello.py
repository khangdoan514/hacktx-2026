from fastapi import APIRouter
from pydantic import BaseModel

router = APIRouter(tags=["hello"])

class HelloResponse(BaseModel):
    message: str

@router.get("/hello", response_model=HelloResponse)
def hello(name: str = "HackTX") -> HelloResponse:
    return HelloResponse(message=f"Hello, {name}! The backend is connected.")

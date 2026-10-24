from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)

def test_health():
    res = client.get("/api/health")
    assert res.status_code == 200
    assert res.json() == {"status": "ok"}

def test_hello():
    res = client.get("/api/hello", params={"name": "Longhorn"})
    assert res.status_code == 200
    assert "Longhorn" in res.json()["message"]

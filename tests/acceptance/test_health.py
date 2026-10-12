"""Health"""
import requests

from conftest import BASE_URL


def test_health_reports_database_and_google():
    """Database is reachable and Google is linked"""
    response = requests.get(f"{BASE_URL}/healthz", timeout=5)
    assert response.status_code == 200
    assert response.json() == {"database": "ok", "google": "ok"}

"""Acceptance tests: drive the running system from outside, like the partner and an admin.

They talk to lead-api over HTTP and read Postgres to check what was stored and synced.
They never import Go code. Start the stack with `make up` first; scripts/test.sh loads .env.
"""
import hashlib
import hmac
import json
import os
import time
import uuid
from urllib.parse import urlsplit, urlunsplit

import psycopg
import pytest
import requests

BASE_URL = os.environ.get("ACCEPTANCE_BASE_URL") or f"http://localhost:{os.environ.get('PORT') or 8080}"
SIGNING_SECRET = os.environ.get("LEAD_SIGNING_SECRET", "")
ADMIN_API_KEY = os.environ.get("ADMIN_API_KEY", "")


def database_url():
    """DATABASE_URL from .env, pointed at the host port compose publishes."""
    url = os.environ.get("ACCEPTANCE_DATABASE_URL") or os.environ.get("DATABASE_URL", "")
    parts = urlsplit(url)
    host = f"{parts.username}:{parts.password}@localhost:{os.environ.get('DB_PORT') or 5432}"
    return urlunsplit(parts._replace(netloc=host))


def sign(body: bytes, secret: str, timestamp: int) -> str:
    mac = hmac.new(secret.encode(), str(timestamp).encode() + b"." + body, hashlib.sha256)
    return "sha256=" + mac.hexdigest()


class Partner:
    """Sends leads the way the partner website does."""

    def send(self, lead: dict, secret=None, timestamp=None, signed=True, tamper=None):
        body = json.dumps(lead).encode()
        timestamp = int(time.time()) if timestamp is None else timestamp
        headers = {"Content-Type": "application/json"}
        if signed:
            headers["X-Timestamp"] = str(timestamp)
            headers["X-Signature"] = sign(body, SIGNING_SECRET if secret is None else secret, timestamp)
        if tamper:
            body = tamper(body)
        return requests.post(f"{BASE_URL}/v1/leads", data=body, headers=headers, timeout=10)


class Admin:
    def move(self, lead_id: str, stage: str, key=None):
        token = ADMIN_API_KEY if key is None else key
        return requests.patch(
            f"{BASE_URL}/v1/leads/{lead_id}/stage",
            json={"stage": stage},
            headers={"Authorization": f"Bearer {token}"} if token else {},
            timeout=10,
        )


class Database:
    """Read-only view of what the system stored."""

    def __init__(self, conn):
        self.conn = conn

    def leads(self, **where):
        column, value = next(iter(where.items()))
        return self.conn.execute(
            f"SELECT external_id, name, email, company, source, stage::text, version FROM leads WHERE {column} = %s",
            (value,),
        ).fetchall()

    def stage(self, external_id):
        return self.leads(external_id=external_id)[0][5]

    def wait_until_synced(self, external_id, timeout=20):
        """Waits for sheets-sync to write the lead's latest version to the sheet."""
        deadline = time.monotonic() + timeout
        while True:
            version, synced, error = self.conn.execute(
                "SELECT l.version, s.synced_version, s.last_error FROM leads l "
                "JOIN lead_sync s ON s.lead_id = l.id WHERE l.external_id = %s",
                (external_id,),
            ).fetchone()
            if synced >= version:
                return version
            if time.monotonic() > deadline:
                pytest.fail(f"version {version} not synced after {timeout}s (synced {synced}, last error: {error})")
            time.sleep(0.5)


@pytest.fixture(scope="session")
def partner():
    return Partner()


@pytest.fixture(scope="session")
def admin():
    return Admin()


@pytest.fixture(scope="session")
def db():
    with psycopg.connect(database_url(), autocommit=True) as conn:
        conn.read_only = True
        yield Database(conn)


@pytest.fixture
def new_lead():
    """Returns a fresh lead with ids and emails no earlier run has used."""
    def make(**fields):
        tag = uuid.uuid4().hex[:8]
        lead = {"id": f"qa-{tag}", "name": "Ada", "email": f"ada.{tag}@example.com",
                "company": "Acme", "source": "acceptance"}
        lead.update(fields)
        return lead
    return make


def health():
    return requests.get(f"{BASE_URL}/healthz", timeout=5).json()


def pytest_sessionstart(session):
    missing = [k for k in ("LEAD_SIGNING_SECRET", "ADMIN_API_KEY", "DATABASE_URL") if not os.environ.get(k)]
    if missing:
        pytest.exit(f"missing settings: {', '.join(missing)}. Run through scripts/test.sh, which loads .env.", returncode=2)
    try:
        health()
    except requests.RequestException:
        pytest.exit(f"lead-api is not reachable at {BASE_URL}. Start it with: make up", returncode=2)


# --- Readable report: one line per test, grouped by area -------------------------

_results = []


def pytest_report_teststatus(report, config):
    # Silence pytest's dots; pytest_runtest_logreport prints the lines instead.
    if report.when == "call" or report.outcome != "passed":
        return report.outcome, "", report.outcome.upper()
    return "", "", ""


def _describe(item):
    doc = (item.function.__doc__ or item.name).strip().splitlines()[0]
    marker = item.get_closest_marker("req")
    return (marker.args[0] if marker else ""), doc


_current_area = [None]


@pytest.hookimpl(wrapper=True)
def pytest_runtest_protocol(item, nextitem):
    area = (item.module.__doc__ or item.module.__name__).strip().splitlines()[0]
    writer = item.config.pluginmanager.get_plugin("terminalreporter")
    if area != _current_area[0]:
        _current_area[0] = area
        writer.write_line("")
        writer.write_line(area, bold=True)
    return (yield)


def pytest_runtest_logreport(report):
    finished = report.when == "call" or (report.when == "setup" and report.outcome != "passed")
    if not finished:
        return
    writer = _config.pluginmanager.get_plugin("terminalreporter")
    req, text = _items[report.nodeid]
    label, color = {"passed": ("PASS", "green"), "failed": ("FAIL", "red"), "skipped": ("SKIP", "yellow")}[report.outcome]
    writer.write(f"  {label} ", **{color: True, "bold": True})
    writer.write_line(f" {req:<5} {text}  ({report.duration:.2f}s)")
    if report.outcome == "skipped" and isinstance(report.longrepr, tuple):
        writer.write_line(f"         {report.longrepr[2]}", yellow=True)
    _results.append(report.outcome)


_items = {}
_config = None


AREA_ORDER = ["test_health", "test_signature", "test_duplicates", "test_email_update", "test_stages", "test_sync"]


def pytest_collection_modifyitems(config, items):
    global _config
    _config = config
    items.sort(key=lambda item: AREA_ORDER.index(item.module.__name__))
    for item in items:
        _items[item.nodeid] = _describe(item)


def pytest_terminal_summary(terminalreporter):
    counts = {k: _results.count(k) for k in ("passed", "failed", "skipped")}
    terminalreporter.write_line("")
    terminalreporter.write_line(
        f"Acceptance: {counts['passed']} passed, {counts['failed']} failed, {counts['skipped']} skipped",
        bold=True, green=counts["failed"] == 0, red=counts["failed"] > 0,
    )

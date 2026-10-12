"""Google Sheets sync (R3.1)"""
import pytest

from conftest import health


@pytest.fixture(autouse=True)
def google_linked():
    if health().get("google") != "ok":
        pytest.skip("Google is not linked; open /auth/google?key=<ADMIN_API_KEY> first")


@pytest.mark.req("R3.1")
def test_new_lead_reaches_the_sheet(partner, db, new_lead):
    """A new lead is written to the Google Sheet"""
    lead = new_lead()
    assert partner.send(lead).status_code == 201
    assert db.wait_until_synced(lead["id"]) == 1


@pytest.mark.req("R3.1")
def test_updated_lead_is_written_again(partner, db, new_lead):
    """An updated lead is written to the sheet again"""
    lead = new_lead()
    assert partner.send(lead).status_code == 201
    db.wait_until_synced(lead["id"])
    assert partner.send({**lead, "company": "Updated Co"}).status_code == 200
    assert db.wait_until_synced(lead["id"]) == 2


@pytest.mark.req("R3.1")
def test_stage_change_reaches_the_sheet(partner, admin, db, new_lead):
    """A stage change is written to the sheet"""
    lead = new_lead()
    assert partner.send(lead).status_code == 201
    assert admin.move(lead["id"], "qualified").status_code == 200
    assert db.wait_until_synced(lead["id"]) == 2

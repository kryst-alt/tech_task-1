"""Deal stages (R2.2)"""
import pytest


@pytest.fixture
def stored_lead(partner, new_lead):
    lead = new_lead()
    assert partner.send(lead).status_code == 201
    return lead


@pytest.mark.req("R2.2")
def test_new_lead_starts_at_new(stored_lead, db):
    """A new lead starts at the New stage"""
    assert db.stage(stored_lead["id"]) == "new"


@pytest.mark.req("R2.2")
def test_lead_moves_forward_to_won(stored_lead, admin, db):
    """A lead moves New, Qualified, Proposal, Won"""
    for stage in ("qualified", "proposal", "won"):
        response = admin.move(stored_lead["id"], stage)
        assert response.status_code == 200, response.text
        assert db.stage(stored_lead["id"]) == stage


@pytest.mark.req("R2.2")
def test_skipping_a_stage_is_refused(stored_lead, admin, db):
    """Skipping a stage is refused"""
    assert admin.move(stored_lead["id"], "proposal").status_code == 409
    assert db.stage(stored_lead["id"]) == "new"


@pytest.mark.req("R2.2")
def test_won_and_lost_are_final(partner, admin, db, new_lead):
    """Won and Lost are final"""
    lost = new_lead()
    assert partner.send(lost).status_code == 201
    assert admin.move(lost["id"], "lost").status_code == 200
    assert admin.move(lost["id"], "qualified").status_code == 409
    assert db.stage(lost["id"]) == "lost"


@pytest.mark.req("R2.2")
def test_partner_update_keeps_the_stage(stored_lead, partner, admin, db):
    """A partner update changes the details but keeps the stage"""
    assert admin.move(stored_lead["id"], "qualified").status_code == 200
    assert partner.send({**stored_lead, "company": "New Co"}).status_code == 200
    assert db.stage(stored_lead["id"]) == "qualified"


@pytest.mark.req("R2.2")
def test_stage_change_needs_admin_key(stored_lead, admin, db):
    """A stage change without the admin key is refused"""
    assert admin.move(stored_lead["id"], "qualified", key="wrong-key").status_code == 401
    assert db.stage(stored_lead["id"]) == "new"

"""Signature check (R1.1, R1.2, R4.1)"""
import time

import pytest


@pytest.mark.req("R1.1")
def test_signed_lead_is_stored(partner, db, new_lead):
    """A correctly signed lead is stored with all five fields"""
    lead = new_lead()
    response = partner.send(lead)
    assert response.status_code == 201, response.text
    assert response.json() == {"result": "create"}
    [(external_id, name, email, company, source, stage, _)] = db.leads(external_id=lead["id"])
    assert (external_id, name, email, company, source) == (lead["id"], lead["name"], lead["email"], lead["company"], lead["source"])


@pytest.mark.req("R1.2")
def test_missing_signature_is_rejected(partner, db, new_lead):
    """A lead without a signature is rejected and not stored"""
    lead = new_lead()
    assert partner.send(lead, signed=False).status_code == 401
    assert db.leads(external_id=lead["id"]) == []


@pytest.mark.req("R1.2")
def test_wrong_secret_is_rejected(partner, db, new_lead):
    """A lead signed with the wrong secret is rejected and not stored"""
    lead = new_lead()
    assert partner.send(lead, secret="not-the-partner-secret").status_code == 401
    assert db.leads(external_id=lead["id"]) == []


@pytest.mark.req("R1.2")
def test_body_changed_after_signing_is_rejected(partner, db, new_lead):
    """A lead whose body changed after signing is rejected"""
    lead = new_lead()
    response = partner.send(lead, tamper=lambda body: body.replace(b"ada.", b"eve."))
    assert response.status_code == 401
    assert db.leads(external_id=lead["id"]) == []


@pytest.mark.req("R1.2")
def test_old_timestamp_is_rejected(partner, db, new_lead):
    """A correctly signed lead from 10 minutes ago is rejected (replay)"""
    lead = new_lead()
    assert partner.send(lead, timestamp=int(time.time()) - 600).status_code == 401
    assert db.leads(external_id=lead["id"]) == []

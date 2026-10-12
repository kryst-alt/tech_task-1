"""Existing email updates the lead (R1.4, R4.3)"""
import pytest


@pytest.mark.req("R1.4")
def test_new_id_with_existing_email_updates_lead(partner, db, new_lead):
    """A new id with a known email updates that lead instead of adding one"""
    first = new_lead()
    assert partner.send(first).status_code == 201
    second = new_lead(email=first["email"], name="Ada Lovelace", company="Analytical Engines")
    response = partner.send(second)
    assert response.status_code == 200
    assert response.json() == {"result": "update"}
    [(external_id, name, _, company, _, _, version)] = db.leads(email=first["email"])
    assert (external_id, name, company, version) == (first["id"], "Ada Lovelace", "Analytical Engines", 2)


@pytest.mark.req("R1.4")
def test_email_match_ignores_case(partner, db, new_lead):
    """The email match ignores upper and lower case"""
    first = new_lead()
    assert partner.send(first).status_code == 201
    assert partner.send(new_lead(email=first["email"].upper(), name="Ada L")).status_code == 200
    assert len(db.leads(email=first["email"])) == 1


@pytest.mark.req("R1.4")
def test_id_and_email_of_different_leads_is_refused(partner, db, new_lead):
    """An id from one lead with the email of another is refused, nothing merged"""
    a, b = new_lead(), new_lead(name="Grace")
    assert partner.send(a).status_code == 201
    assert partner.send(b).status_code == 201
    assert partner.send(new_lead(id=a["id"], email=b["email"])).status_code == 409
    assert db.leads(external_id=a["id"])[0][2] == a["email"]
    assert db.leads(external_id=b["id"])[0][2] == b["email"]

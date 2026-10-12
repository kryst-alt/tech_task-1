"""Duplicates (R1.3, R4.2)"""
from concurrent.futures import ThreadPoolExecutor

import pytest


@pytest.mark.req("R1.3")
def test_same_lead_twice_is_stored_once(partner, db, new_lead):
    """The same lead sent twice is stored once"""
    lead = new_lead()
    assert partner.send(lead).status_code == 201
    second = partner.send(lead)
    assert second.status_code == 200
    assert second.json() == {"result": "unchanged"}
    assert len(db.leads(external_id=lead["id"])) == 1


@pytest.mark.req("R1.3")
def test_ten_identical_requests_at_once_are_stored_once(partner, db, new_lead):
    """Ten identical leads sent at the same moment are stored once"""
    lead = new_lead()
    with ThreadPoolExecutor(max_workers=10) as pool:
        statuses = sorted(r.status_code for r in pool.map(lambda _: partner.send(lead), range(10)))
    assert statuses == [200] * 9 + [201]
    assert len(db.leads(email=lead["email"])) == 1

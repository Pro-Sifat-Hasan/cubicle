import json

import pytest

from cubicle_agents.staff.roster import (
    DEFAULT_ROSTER,
    Employee,
    RosterError,
    load_roster,
    manager_of,
    specialists_of,
    validate,
    write_default_roster,
)


def test_default_roster_is_valid():
    roster = validate(list(DEFAULT_ROSTER))
    assert manager_of(roster).id == "manager"
    assert len(specialists_of(roster)) >= 1


def test_load_roster_defaults_when_no_file(tmp_path):
    assert load_roster(tmp_path) == list(DEFAULT_ROSTER)


def test_custom_roster_round_trip(tmp_path):
    write_default_roster(tmp_path / "staff.json")
    assert load_roster(tmp_path) == list(DEFAULT_ROSTER)


@pytest.mark.parametrize(
    "roster, message",
    [
        ([], "empty"),
        ([Employee("a", "A", "T", "specialist", "s", "p")], "manager"),
        ([Employee("m", "M", "T", "manager", "s", "p")], "specialist"),
        ([Employee("m", "M", "T", "manager", "s", "p"), Employee("m", "B", "T", "specialist", "s", "p")], "unique"),
        ([Employee("Bad Id", "M", "T", "manager", "s", "p"), Employee("b", "B", "T", "specialist", "s", "p")],
         "invalid"),
    ],
)
def test_validate_rejects_bad_rosters(roster, message):
    with pytest.raises(RosterError, match=message):
        validate(roster)


def test_load_roster_rejects_unknown_fields(tmp_path):
    (tmp_path / "staff.json").write_text(json.dumps([{"id": "x", "bogus": 1}]), encoding="utf-8")
    with pytest.raises(RosterError):
        load_roster(tmp_path)

"""Tests for cats-check-fixtures.py. Run: uv run --with pytest --with pyyaml pytest scripts/test_cats_check_fixtures.py"""

import importlib.util
import json
from pathlib import Path

import pytest

_spec = importlib.util.spec_from_file_location(
    "cats_check_fixtures", Path(__file__).with_name("cats-check-fixtures.py"))
mod = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mod)


def write(tmp_path, doc):
    p = tmp_path / "cats-fixtures.json"
    p.write_text(doc if isinstance(doc, str) else json.dumps(doc))
    return p


def test_manifest_present_probes_every_key(tmp_path):
    p = write(tmp_path, {"fixtures": [
        {"key": "diagram_id", "verify_url": "/threat_models/t/diagrams/d"},
        {"key": "survey_id", "verify_url": "/admin/surveys/s"},
        {"key": "threat_model_id", "verify_url": "/threat_models/t"},
    ]})
    targets = mod.load_manifest(p)
    seen = []
    statuses = {"/threat_models/t/diagrams/d": 404, "/admin/surveys/s": 403, "/threat_models/t": 200}

    def stub(server, path, token):
        seen.append(path)
        return statuses[path]

    casualties, survivors = mod.classify("http://x", "tok", targets, stub)
    assert [k for k, _ in targets] == ["diagram_id", "survey_id", "threat_model_id"]
    assert sorted(seen) == sorted(statuses)
    assert [c[0] for c in casualties] == ["diagram_id"]  # 404 is a casualty
    assert [s[0] for s in survivors] == ["survey_id", "threat_model_id"]  # 403 is not


def test_gone_410_is_casualty_and_zero_is_not():
    c, s = mod.classify("h", "t", [("a", "/a"), ("b", "/b")], lambda *_: 410 if _[1] == "/a" else 0)
    assert [x[0] for x in c] == ["a"] and [x[0] for x in s] == ["b"]


def test_manifest_missing_falls_back_to_anchors():
    targets = mod.anchor_targets({"threat_model_id": "abc", "team_id": "00000000-0000-0000-0000-000000000000",
                                  "diagram_id": "ignored"})
    assert targets == [("threat_model_id", "/threat_models/abc")]


@pytest.mark.parametrize("doc", [
    "not json",
    "[]",
    {"nofixtures": 1},
    {"fixtures": [{"key": "k"}]},
    {"fixtures": [{"key": "k", "verify_url": "http://abs/"}]},
    {"fixtures": ["str"]},
])
def test_malformed_manifest_fails_clearly(tmp_path, doc):
    with pytest.raises(mod.ManifestError):
        mod.load_manifest(write(tmp_path, doc))


def test_main_reports_malformed_manifest_and_does_not_fail_run(tmp_path, monkeypatch, capsys):
    p = write(tmp_path, "garbage")
    monkeypatch.setattr("sys.argv", ["x", "--server", "http://x", "--fixtures", str(p)])
    assert mod.main() == 0
    assert "cats-check-fixtures:" in capsys.readouterr().err


def _db(tmp_path, rows):
    """Minimal CATS results DB with the tables report_consumed() joins."""
    import sqlite3
    p = tmp_path / "cats.db"
    con = sqlite3.connect(p)
    con.executescript("""
        CREATE TABLE fuzzers (id INTEGER PRIMARY KEY, name TEXT);
        CREATE TABLE http_methods (id INTEGER PRIMARY KEY, method TEXT);
        CREATE TABLE tests (id INTEGER PRIMARY KEY, test_number INTEGER, fuzzer_id INTEGER);
        CREATE TABLE requests (test_id INTEGER, http_method_id INTEGER, url TEXT);
        CREATE TABLE responses (test_id INTEGER, response_code INTEGER);
    """)
    fz, mt = {}, {}
    for i, (number, fuzzer, method, url, code) in enumerate(rows, start=1):
        fid = fz.setdefault(fuzzer, len(fz) + 1)
        mid = mt.setdefault(method, len(mt) + 1)
        con.execute("INSERT OR IGNORE INTO fuzzers VALUES (?, ?)", (fid, fuzzer))
        con.execute("INSERT OR IGNORE INTO http_methods VALUES (?, ?)", (mid, method))
        con.execute("INSERT INTO tests VALUES (?, ?, ?)", (i, number, fid))
        con.execute("INSERT INTO requests VALUES (?, ?, ?)", (i, mid, url))
        con.execute("INSERT INTO responses VALUES (?, ?)", (i, code))
    con.commit()
    con.close()
    return p


S = "http://rp2:30080"
DECOY = "/threat_models/decoy"


def test_consumed_counts_later_404s_on_resource_and_children():
    rows = [
        (1, "HappyPath", "GET", S + DECOY, 200),
        (2, "DummyContentTypeHeaders", "DELETE", S + DECOY + "?x=1", 204),
        (3, "HappyPath", "GET", S + DECOY, 404),
        (4, "HappyPath", "PUT", S + DECOY + "/assets/a", 404),
        (5, "HappyPath", "GET", S + DECOY + "x", 404),     # sibling prefix, not nested
        (6, "HappyPath", "GET", S + "/threat_models/other", 404),
    ]
    assert mod.consumed_mid_run(rows) == [(DECOY, 2, "DummyContentTypeHeaders", 2)]


def test_consumed_ignores_expected_404_and_404_before_delete():
    rows = [
        (1, "HappyPath", "GET", S + DECOY, 404),
        (2, "HappyPath", "DELETE", S + DECOY, 204),
        (3, "CheckDeletedResourcesNotAvailable", "GET", S + DECOY, 404),
    ]
    assert mod.consumed_mid_run(rows) == []


def test_consumed_ignores_failed_delete():
    rows = [
        (1, "DummyContentTypeHeaders", "DELETE", S + DECOY, 415),
        (2, "HappyPath", "GET", S + DECOY, 404),
    ]
    assert mod.consumed_mid_run(rows) == []


def test_consumed_ranks_by_damage():
    rows = [
        (1, "A", "DELETE", S + "/a", 204),
        (2, "B", "DELETE", S + "/b", 200),
        (3, "X", "GET", S + "/a", 404),
        (4, "X", "GET", S + "/b", 404),
        (5, "X", "GET", S + "/b/c", 404),
    ]
    assert [r[0] for r in mod.consumed_mid_run(rows)] == ["/b", "/a"]


def test_report_consumed_reads_db_and_prints(tmp_path, capsys):
    db = _db(tmp_path, [
        (1, "DummyContentTypeHeaders", "DELETE", S + DECOY, 204),
        (2, "HappyPath", "GET", S + DECOY, 404),
    ])
    assert mod.report_consumed(db) == [(DECOY, 1, "DummyContentTypeHeaders", 1)]
    out = capsys.readouterr().out
    assert "deleted at test 1 by DummyContentTypeHeaders: " + DECOY in out


def test_report_consumed_unreadable_db_does_not_raise(tmp_path, capsys):
    bad = tmp_path / "not-a-db"
    bad.write_text("garbage")
    assert mod.report_consumed(bad) == []
    assert "cannot read" in capsys.readouterr().err


def test_main_reports_consumed_without_server(tmp_path, monkeypatch, capsys):
    db = _db(tmp_path, [
        (1, "D", "DELETE", S + DECOY, 204),
        (2, "H", "GET", S + DECOY, 404),
    ])
    monkeypatch.setattr(mod.sys, "argv", ["cats-check-fixtures.py"])
    monkeypatch.setenv("CATS_DB", str(db))
    monkeypatch.delenv("CATS_SERVER", raising=False)
    assert mod.main() == 0
    captured = capsys.readouterr()
    assert DECOY in captured.out
    assert "no --server" in captured.err

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

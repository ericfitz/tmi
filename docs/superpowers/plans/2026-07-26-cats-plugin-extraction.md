# CATS Plugin Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract TMI's CATS API-fuzzing pipeline into a repo-agnostic `cats` Claude Code plugin, with per-repo configuration and declarative false-positive rules, losing no TMI functionality.

**Architecture:** A single Python entrypoint (`cats_tool.py`) over a package of focused modules: config discovery/validation, a declarative rule matcher, a report-JSON→SQLite parser, a re-runnable classifier, a hook-driven runner, and an HTML reporter. Repo-specific behavior (seeding, token acquisition, environment prep) is expressed as opaque shell hooks in a gitignored `.local/cats/config.yaml`; the false-positive rules live in a committed YAML file in the consuming repo. Five skills wrap the tool.

**Tech Stack:** Python 3.11+, stdlib `sqlite3` + `argparse` + `unittest`, PyYAML, uv for script execution, CATS (Endava) as the fuzzer.

## Global Constraints

- **Two repos.** Plugin work: `~/Projects/skills`, branch `feat/cats-plugin`. TMI work: `/Users/efitz/Projects/tmi`, branch `dev/1.6.0/cats-plugin-extraction` (already exists, spec committed).
- **No TMI-specific string may appear in the plugin.** No `tmi`, `charlie`, `threat_model`, `8079`, `tmi-redis`, `api-schema/`, `oauth stub`. Task 8 greps for these.
- **Skills-repo test convention:** stdlib `unittest` classes, `sys.dont_write_bytecode = True`, `sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cats" / "scripts"))`. Run with `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_<x>.py -v`.
- **Python style:** type hints on public functions, `pathlib.Path` for paths, no `print` in library modules (return data, let the CLI print).
- **Secrets:** tokens never in argv, never in hook env, never logged. Any log line that could contain a token passes through `redact(text, token)`.
- **Rule ordering is load-bearing.** Rules evaluate in file order, first match wins. The legacy `detect_false_positive()` is a long if-chain where earlier rules shadow later ones; porting must preserve that order exactly.
- **Case semantics:** `equals`/`in`/`not_equals` are case-sensitive. `contains`/`contains_any`/`contains_all`/`starts_with`/`starts_with_any`/`ends_with`/`matches` are case-insensitive. This mirrors the legacy code, which lowercases text before substring checks but compares fuzzer names exactly.
- **Merge gate:** the ported rule set must reproduce legacy classification on all 121,940 corpus tests (Task 10) before any legacy script is deleted (Task 13).
- **Conventional commits**, one per task minimum. TMI commits end with the `Co-Authored-By` / `Claude-Session` trailers from CLAUDE.md; skills-repo commits do not need them.

## Key domain facts (read before starting)

**CATS result JSON** (`<report>/Test<N>.json`) top-level keys: `testId` (`"Test 1"`), `traceId`, `fuzzer`, `path`, `contractPath`, `fullRequestPath`, `scenario`, `expectedResult`, `result` (`success|warn|error`), `resultReason`, `resultDetails`, `resultIgnoreDetails`, `server`, `request`, `response`.

- `request`: `httpMethod`, `url`, `timestamp`, `payload`, `headers` (list of `{key, value}`)
- `response`: `httpMethod`, `responseCode`, `responseTimeInMs`, `numberOfWordsInResponse`, `numberOfLinesInResponse`, `contentLengthInBytes`, `responseContentType`, `jsonBody`, `headers` (list of `{key, value}`)

**There is no `responseBody` key.** The legacy code reads `response.responseBody` in 7 places across 5 rules (`TRANSFER_ENCODING_501`, `EMPTY_BODY_REQUIRED_FIELDS`, `ADMIN_SETTINGS_RESERVED`, `PATH_PARAM_VALIDATION`, `NO_BODY_ENDPOINT`); those sub-conditions always evaluate against `''` and can never match. Task 9 ports them **faithfully as dead** (omitted, with a note) so Task 10's diff is clean; Task 10 produces a report of these for a later, deliberate decision.

**Corpus:** `/Users/efitz/Projects/tmi/test/outputs/cats/report/` holds 121,940 `Test*.json` from the 2026-06-30 run. Do not delete or modify it until Task 14 is signed off.

## File structure

**Plugin (`~/Projects/skills/cats/`)**

| File | Responsibility |
| --- | --- |
| `.claude-plugin/plugin.json` | plugin manifest |
| `scripts/cats_tool.py` | argparse CLI; the only file that prints |
| `scripts/catslib/config.py` | config discovery, parsing, validation |
| `scripts/catslib/rules.py` | rule loading + the match engine |
| `scripts/catslib/schema.sql` | full DDL (tables, indexes, views) |
| `scripts/catslib/parse.py` | report JSON → SQLite |
| `scripts/catslib/classify.py` | apply rules to a DB, compute deltas |
| `scripts/catslib/runner.py` | hooks, token, CATS argv, pipeline |
| `scripts/catslib/report.py` | self-contained HTML report |
| `skills/{init,run,report,analyze,fp}/SKILL.md` | the five skills |
| `agents/cats-run.md` | subagent definition for the long run |

**Tests (`~/Projects/skills/tests/`)**: `test_cats_config.py`, `test_cats_rules.py`, `test_cats_parse.py`, `test_cats_classify.py`, `test_cats_runner.py`.

**TMI**: `.local/cats/config.yaml` (gitignored), `test/cats/false-positives.yaml` (committed), `scripts/cats-token.py`, `scripts/cats-prep.py`, Makefile wrappers. Deletions: `scripts/run-cats-fuzz.py`, `scripts/parse_cats_results.py`, `scripts/query-cats-results.py`.

---

## Task 1: Plugin scaffold and configuration module

**Files:**
- Create: `~/Projects/skills/cats/.claude-plugin/plugin.json`
- Create: `~/Projects/skills/cats/scripts/catslib/__init__.py`
- Create: `~/Projects/skills/cats/scripts/catslib/config.py`
- Test: `~/Projects/skills/tests/test_cats_config.py`

**Interfaces:**
- Consumes: nothing.
- Produces: `find_config(start: Path) -> Path | None`, `load_config(path: Path) -> Config`, `ConfigError`, and the `Config` dataclass with fields `repo_root, config_path, spec, server, health_url, results_dir, false_positives, retain_raw_report, allow_suppressing_5xx, identities: dict[str, Identity], default_identity, auth_header, auth_template, hooks: Hooks, cats: CatsOptions`. `Identity(name, token_cmd)`. `Hooks(seed, pre_run, post_run)`. `CatsOptions(http_methods, max_requests_per_minute, ref_data, skip_field_format, skip_field, skip_fuzzers, skip_fuzzers_for_extension, extra_args)`.

- [ ] **Step 1: Create the branch**

```bash
cd ~/Projects/skills && git checkout -b feat/cats-plugin
mkdir -p cats/.claude-plugin cats/scripts/catslib cats/skills cats/agents
```

- [ ] **Step 2: Write the plugin manifest**

`cats/.claude-plugin/plugin.json`:

```json
{
  "name": "cats",
  "version": "0.1.0",
  "description": "Portable CATS API fuzzing toolkit: bootstrap per-repo config (init), run fuzz campaigns via config-declared hooks (run), query and render results from SQLite (report), triage true positives into a remediation plan (analyze), and manage declarative false-positive rules (fp).",
  "author": { "name": "efitz" }
}
```

- [ ] **Step 3: Write the failing tests**

`tests/test_cats_config.py`:

```python
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cats" / "scripts"))

from catslib import config as cfg

MINIMAL = """
version: 1
spec: openapi.json
server: http://localhost:8080
results_dir: test/results/cats
false_positives: test/cats/false-positives.yaml
identities:
  admin: {token_cmd: "echo tok"}
default_identity: admin
"""


class TestFindConfig(unittest.TestCase):
    def test_finds_config_by_walking_up(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / ".local" / "cats").mkdir(parents=True)
            target = root / ".local" / "cats" / "config.yaml"
            target.write_text(MINIMAL)
            deep = root / "a" / "b" / "c"
            deep.mkdir(parents=True)
            self.assertEqual(cfg.find_config(deep), target)

    def test_returns_none_when_absent(self):
        with tempfile.TemporaryDirectory() as d:
            self.assertIsNone(cfg.find_config(Path(d)))


class TestLoadConfig(unittest.TestCase):
    def _write(self, body):
        d = tempfile.mkdtemp()
        root = Path(d)
        (root / ".local" / "cats").mkdir(parents=True)
        p = root / ".local" / "cats" / "config.yaml"
        p.write_text(body)
        return p

    def test_defaults_applied(self):
        c = cfg.load_config(self._write(MINIMAL))
        self.assertEqual(c.default_identity, "admin")
        self.assertEqual(c.auth_header, "Authorization")
        self.assertEqual(c.auth_template, "Bearer {token}")
        self.assertFalse(c.retain_raw_report)
        self.assertFalse(c.allow_suppressing_5xx)
        self.assertEqual(c.cats.max_requests_per_minute, 3000)
        self.assertEqual(c.cats.http_methods, ["POST", "PUT", "GET", "DELETE", "PATCH"])
        self.assertIsNone(c.hooks.seed)

    def test_paths_resolve_against_repo_root(self):
        p = self._write(MINIMAL)
        c = cfg.load_config(p)
        self.assertEqual(c.repo_root, p.parents[2])
        self.assertEqual(c.spec, c.repo_root / "openapi.json")
        self.assertEqual(c.results_dir, c.repo_root / "test" / "results" / "cats")

    def test_health_url_defaults_to_server(self):
        self.assertEqual(cfg.load_config(self._write(MINIMAL)).health_url,
                         "http://localhost:8080")

    def test_missing_required_key_names_the_key(self):
        body = MINIMAL.replace("server: http://localhost:8080\n", "")
        with self.assertRaises(cfg.ConfigError) as ctx:
            cfg.load_config(self._write(body))
        self.assertIn("server", str(ctx.exception))

    def test_unknown_top_level_key_rejected(self):
        with self.assertRaises(cfg.ConfigError) as ctx:
            cfg.load_config(self._write(MINIMAL + "\nbogus: 1\n"))
        self.assertIn("bogus", str(ctx.exception))

    def test_default_identity_must_exist(self):
        body = MINIMAL.replace("default_identity: admin", "default_identity: nobody")
        with self.assertRaises(cfg.ConfigError) as ctx:
            cfg.load_config(self._write(body))
        self.assertIn("nobody", str(ctx.exception))

    def test_skip_fuzzers_for_extension_shape_validated(self):
        body = MINIMAL + """
cats:
  skip_fuzzers_for_extension:
    - {extension: x-public-endpoint, value: "true", fuzzers: [BypassAuthentication]}
"""
        c = cfg.load_config(self._write(body))
        self.assertEqual(c.cats.skip_fuzzers_for_extension[0]["fuzzers"],
                         ["BypassAuthentication"])

    def test_unsupported_version_rejected(self):
        body = MINIMAL.replace("version: 1", "version: 99")
        with self.assertRaises(cfg.ConfigError):
            cfg.load_config(self._write(body))


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_config.py -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'catslib'`

- [ ] **Step 5: Implement `config.py`**

Create `cats/scripts/catslib/__init__.py` (empty) and `cats/scripts/catslib/config.py`:

```python
"""Discovery, parsing and validation of .local/cats/config.yaml."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml

CONFIG_RELPATH = Path(".local") / "cats" / "config.yaml"
SUPPORTED_VERSIONS = {1}

TOP_LEVEL_KEYS = {
    "version", "spec", "server", "health_url", "results_dir", "false_positives",
    "retain_raw_report", "allow_suppressing_5xx", "identities", "default_identity",
    "auth", "hooks", "cats",
}
CATS_KEYS = {
    "http_methods", "max_requests_per_minute", "ref_data", "skip_field_format",
    "skip_field", "skip_fuzzers", "skip_fuzzers_for_extension", "extra_args",
}
HOOK_KEYS = {"seed", "pre_run", "post_run"}
REQUIRED = ("spec", "server", "results_dir", "false_positives", "identities")


class ConfigError(Exception):
    """Raised with an actionable message when a config is missing or invalid."""


@dataclass(frozen=True)
class Identity:
    name: str
    token_cmd: str


@dataclass(frozen=True)
class Hooks:
    seed: str | None = None
    pre_run: str | None = None
    post_run: str | None = None


@dataclass(frozen=True)
class CatsOptions:
    http_methods: list[str] = field(default_factory=lambda: ["POST", "PUT", "GET", "DELETE", "PATCH"])
    max_requests_per_minute: int = 3000
    ref_data: Path | None = None
    skip_field_format: list[str] = field(default_factory=list)
    skip_field: list[str] = field(default_factory=list)
    skip_fuzzers: list[str] = field(default_factory=list)
    skip_fuzzers_for_extension: list[dict[str, Any]] = field(default_factory=list)
    extra_args: list[str] = field(default_factory=list)


@dataclass(frozen=True)
class Config:
    repo_root: Path
    config_path: Path
    spec: Path
    server: str
    health_url: str
    results_dir: Path
    false_positives: Path
    retain_raw_report: bool
    allow_suppressing_5xx: bool
    identities: dict[str, Identity]
    default_identity: str
    auth_header: str
    auth_template: str
    hooks: Hooks
    cats: CatsOptions

    def identity(self, name: str | None) -> Identity:
        key = name or self.default_identity
        if key not in self.identities:
            raise ConfigError(
                f"unknown identity {key!r}; configured: {sorted(self.identities)}"
            )
        return self.identities[key]


def find_config(start: Path) -> Path | None:
    """Walk up from *start* looking for .local/cats/config.yaml."""
    current = start.resolve()
    for directory in [current, *current.parents]:
        candidate = directory / CONFIG_RELPATH
        if candidate.is_file():
            return candidate
    return None


def _reject_unknown(keys, allowed, where: str) -> None:
    unknown = sorted(set(keys) - allowed)
    if unknown:
        raise ConfigError(f"unknown key(s) in {where}: {', '.join(unknown)}")


def load_config(path: Path) -> Config:
    try:
        raw = yaml.safe_load(path.read_text()) or {}
    except yaml.YAMLError as exc:
        raise ConfigError(f"{path}: invalid YAML: {exc}") from exc
    if not isinstance(raw, dict):
        raise ConfigError(f"{path}: top level must be a mapping")

    version = raw.get("version", 1)
    if version not in SUPPORTED_VERSIONS:
        raise ConfigError(
            f"{path}: unsupported version {version!r}; supported: {sorted(SUPPORTED_VERSIONS)}"
        )
    _reject_unknown(raw, TOP_LEVEL_KEYS, str(path))
    for key in REQUIRED:
        if not raw.get(key):
            raise ConfigError(f"{path}: missing required key {key!r}")

    # repo_root is the directory containing .local/
    repo_root = path.parents[2]

    identities_raw = raw["identities"]
    if not isinstance(identities_raw, dict) or not identities_raw:
        raise ConfigError(f"{path}: 'identities' must be a non-empty mapping")
    identities: dict[str, Identity] = {}
    for name, spec in identities_raw.items():
        if not isinstance(spec, dict) or not spec.get("token_cmd"):
            raise ConfigError(f"{path}: identity {name!r} needs a 'token_cmd'")
        identities[name] = Identity(name=name, token_cmd=spec["token_cmd"])

    default_identity = raw.get("default_identity") or next(iter(identities))
    if default_identity not in identities:
        raise ConfigError(
            f"{path}: default_identity {default_identity!r} is not defined in 'identities'"
        )

    auth = raw.get("auth") or {}
    _reject_unknown(auth, {"header", "template"}, "auth")

    hooks_raw = raw.get("hooks") or {}
    _reject_unknown(hooks_raw, HOOK_KEYS, "hooks")
    hooks = Hooks(
        seed=hooks_raw.get("seed") or None,
        pre_run=hooks_raw.get("pre_run") or None,
        post_run=hooks_raw.get("post_run") or None,
    )

    cats_raw = raw.get("cats") or {}
    _reject_unknown(cats_raw, CATS_KEYS, "cats")
    for entry in cats_raw.get("skip_fuzzers_for_extension", []):
        if not isinstance(entry, dict) or not {"extension", "fuzzers"} <= set(entry):
            raise ConfigError(
                f"{path}: each skip_fuzzers_for_extension entry needs 'extension' and 'fuzzers'"
            )
    ref_data = cats_raw.get("ref_data")
    cats_opts = CatsOptions(
        http_methods=cats_raw.get("http_methods") or ["POST", "PUT", "GET", "DELETE", "PATCH"],
        max_requests_per_minute=int(cats_raw.get("max_requests_per_minute", 3000)),
        ref_data=(repo_root / ref_data) if ref_data else None,
        skip_field_format=cats_raw.get("skip_field_format") or [],
        skip_field=cats_raw.get("skip_field") or [],
        skip_fuzzers=cats_raw.get("skip_fuzzers") or [],
        skip_fuzzers_for_extension=cats_raw.get("skip_fuzzers_for_extension") or [],
        extra_args=cats_raw.get("extra_args") or [],
    )

    return Config(
        repo_root=repo_root,
        config_path=path,
        spec=repo_root / raw["spec"],
        server=raw["server"].rstrip("/"),
        health_url=(raw.get("health_url") or raw["server"]).rstrip("/"),
        results_dir=repo_root / raw["results_dir"],
        false_positives=repo_root / raw["false_positives"],
        retain_raw_report=bool(raw.get("retain_raw_report", False)),
        allow_suppressing_5xx=bool(raw.get("allow_suppressing_5xx", False)),
        identities=identities,
        default_identity=default_identity,
        auth_header=auth.get("header") or "Authorization",
        auth_template=auth.get("template") or "Bearer {token}",
        hooks=hooks,
        cats=cats_opts,
    )
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_config.py -v`
Expected: PASS (10 tests)

- [ ] **Step 7: Commit**

```bash
cd ~/Projects/skills
git add cats/.claude-plugin/plugin.json cats/scripts/catslib/ tests/test_cats_config.py
git commit -m "feat(cats): plugin scaffold and config module"
```

---

## Task 2: Declarative rule engine

**Files:**
- Create: `~/Projects/skills/cats/scripts/catslib/rules.py`
- Test: `~/Projects/skills/tests/test_cats_rules.py`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces:
  - `Rule` dataclass: `id: str, why: str, when: dict | None, any_of: list[dict] | None, enabled: bool, tags: list[str], order_index: int`
  - `RuleError(Exception)`
  - `load_rules(path: Path) -> list[Rule]`
  - `match_rule(rule: Rule, record: dict) -> bool`
  - `classify_record(rules: list[Rule], record: dict, *, allow_5xx: bool) -> tuple[bool, str | None, str | None]` returning `(is_false_positive, rule_id, violation_rule_id)`. `violation_rule_id` is set (and `is_false_positive` forced `False`) when a rule matched a 5xx response and `allow_5xx` is False.
  - `FIELDS: frozenset[str]` — the field vocabulary.

**The normalized record** every consumer builds (Tasks 3 and 4 both produce this shape):

```python
{
  "result": "error",                    # success | warn | error
  "response_code": 400,                 # int
  "fuzzer": "HappyPath",
  "path": "/threat_models",
  "contract_path": "/threat_models",
  "method": "POST",
  "url": "http://localhost:8080/threat_models",
  "scenario": "...",
  "result_reason": "...",               # '' when null
  "result_details": "...",              # '' when null
  "response_body": '{"error":"x"}',     # json.dumps of json_body, '' when absent
  "response_content_type": "application/json",
  "request_body": "...",                # request.payload, '' when absent
  "json_body": {...},                   # parsed dict/list, or None
  "request_headers": {"accept": "application/json"},   # keys lowercased
}
```

- [ ] **Step 1: Write the failing tests**

`tests/test_cats_rules.py`:

```python
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cats" / "scripts"))

from catslib import rules as R


def rec(**over):
    base = {
        "result": "error", "response_code": 400, "fuzzer": "HappyPath",
        "path": "/things", "contract_path": "/things", "method": "POST",
        "url": "http://h/things", "scenario": "s", "result_reason": "",
        "result_details": "", "response_body": "", "response_content_type": "application/json",
        "request_body": "", "json_body": None, "request_headers": {},
    }
    base.update(over)
    return base


def rules_from(text):
    with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as fh:
        fh.write(text)
    return R.load_rules(Path(fh.name))


class TestOperators(unittest.TestCase):
    def test_bare_scalar_is_equals(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {response_code: 400}\n")[0]
        self.assertTrue(R.match_rule(r, rec(response_code=400)))
        self.assertFalse(R.match_rule(r, rec(response_code=404)))

    def test_in_operator(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {response_code: {in: [401, 403]}}\n")[0]
        self.assertTrue(R.match_rule(r, rec(response_code=403)))
        self.assertFalse(R.match_rule(r, rec(response_code=400)))

    def test_contains_any_is_case_insensitive(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {result_reason: {contains_any: [unauthorized]}}\n")[0]
        self.assertTrue(R.match_rule(r, rec(result_reason="UNAUTHORIZED access")))

    def test_equals_is_case_sensitive(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {fuzzer: HappyPath}\n")[0]
        self.assertTrue(R.match_rule(r, rec(fuzzer="HappyPath")))
        self.assertFalse(R.match_rule(r, rec(fuzzer="happypath")))

    def test_in_is_case_sensitive(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {fuzzer: {in: [HappyPath]}}\n")[0]
        self.assertFalse(R.match_rule(r, rec(fuzzer="happypath")))

    def test_contains_all(self):
        r = rules_from('version: 1\nrules:\n  - id: A\n    why: w\n    when: {path: {contains_all: ["/admin/", "/metadata"]}}\n')[0]
        self.assertTrue(R.match_rule(r, rec(path="/admin/surveys/1/metadata")))
        self.assertFalse(R.match_rule(r, rec(path="/admin/surveys/1")))

    def test_starts_with_any_and_ends_with(self):
        r = rules_from('version: 1\nrules:\n  - id: A\n    why: w\n    when: {path: {starts_with_any: ["/admin/", "/me/"]}}\n')[0]
        self.assertTrue(R.match_rule(r, rec(path="/me/preferences")))
        r2 = rules_from('version: 1\nrules:\n  - id: A\n    why: w\n    when: {url: {ends_with: "/"}}\n')[0]
        self.assertTrue(R.match_rule(r2, rec(url="http://h/things/")))

    def test_matches_regex_case_insensitive(self):
        r = rules_from('version: 1\nrules:\n  - id: A\n    why: w\n    when: {result_reason: {matches: "code: 4\\\\d\\\\d"}}\n')[0]
        self.assertTrue(R.match_rule(r, rec(result_reason="Unexpected Response CODE: 404")))

    def test_exists_operator(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {json_body.error_description: {exists: true}}\n")[0]
        self.assertTrue(R.match_rule(r, rec(json_body={"error_description": "bad"})))
        self.assertFalse(R.match_rule(r, rec(json_body={})))

    def test_not_equals(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {method: {not_equals: GET}}\n")[0]
        self.assertTrue(R.match_rule(r, rec(method="POST")))
        self.assertFalse(R.match_rule(r, rec(method="GET")))


class TestVirtualFields(unittest.TestCase):
    def test_any_text_concatenates_reason_and_details(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {any_text: {contains: forbidden}}\n")[0]
        self.assertTrue(R.match_rule(r, rec(result_details="Forbidden")))
        self.assertTrue(R.match_rule(r, rec(result_reason="forbidden")))
        self.assertFalse(R.match_rule(r, rec()))

    def test_dotted_json_body_path(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {json_body.error_description: {contains: enum_values}}\n")[0]
        self.assertTrue(R.match_rule(r, rec(json_body={"error_description": "bad ENUM_VALUES here"})))
        self.assertFalse(R.match_rule(r, rec(json_body={"error_description": "other"})))

    def test_nested_dotted_path(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {json_body.error.code: {equals: E1}}\n")[0]
        self.assertTrue(R.match_rule(r, rec(json_body={"error": {"code": "E1"}})))

    def test_request_header_lookup_is_case_insensitive_on_name(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {request_header.Transfer-Encoding: {contains: chunked}}\n")[0]
        self.assertTrue(R.match_rule(r, rec(request_headers={"transfer-encoding": "chunked"})))


class TestConjunctionAndDisjunction(unittest.TestCase):
    def test_when_keys_are_anded(self):
        r = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {response_code: 409, method: POST}\n")[0]
        self.assertTrue(R.match_rule(r, rec(response_code=409, method="POST")))
        self.assertFalse(R.match_rule(r, rec(response_code=409, method="GET")))

    def test_any_of_is_ored(self):
        text = ("version: 1\nrules:\n  - id: A\n    why: w\n"
                "    any_of:\n      - {response_code: 429}\n      - {response_code: 503}\n")
        r = rules_from(text)[0]
        self.assertTrue(R.match_rule(r, rec(response_code=429)))
        self.assertTrue(R.match_rule(r, rec(response_code=503)))
        self.assertFalse(R.match_rule(r, rec(response_code=500)))


class TestClassify(unittest.TestCase):
    def _two(self):
        return rules_from(
            "version: 1\nrules:\n"
            "  - id: FIRST\n    why: w\n    when: {response_code: 400}\n"
            "  - id: SECOND\n    why: w\n    when: {fuzzer: HappyPath}\n"
        )

    def test_first_match_wins(self):
        is_fp, rule_id, _ = R.classify_record(self._two(), rec(), allow_5xx=False)
        self.assertTrue(is_fp)
        self.assertEqual(rule_id, "FIRST")

    def test_later_rule_used_when_first_misses(self):
        is_fp, rule_id, _ = R.classify_record(self._two(), rec(response_code=404), allow_5xx=False)
        self.assertTrue(is_fp)
        self.assertEqual(rule_id, "SECOND")

    def test_no_match(self):
        is_fp, rule_id, _ = R.classify_record(
            self._two(), rec(response_code=404, fuzzer="Other"), allow_5xx=False)
        self.assertFalse(is_fp)
        self.assertIsNone(rule_id)

    def test_only_error_and_warn_are_classified(self):
        is_fp, rule_id, _ = R.classify_record(self._two(), rec(result="success"), allow_5xx=False)
        self.assertFalse(is_fp)
        self.assertIsNone(rule_id)

    def test_disabled_rules_are_skipped(self):
        rs = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    enabled: false\n    when: {response_code: 400}\n")
        is_fp, rule_id, _ = R.classify_record(rs, rec(), allow_5xx=False)
        self.assertFalse(is_fp)
        self.assertIsNone(rule_id)

    def test_5xx_suppression_refused_by_default(self):
        rs = rules_from("version: 1\nrules:\n  - id: BAD\n    why: w\n    when: {fuzzer: HappyPath}\n")
        is_fp, rule_id, violation = R.classify_record(rs, rec(response_code=500), allow_5xx=False)
        self.assertFalse(is_fp)
        self.assertIsNone(rule_id)
        self.assertEqual(violation, "BAD")

    def test_5xx_suppression_allowed_when_opted_in(self):
        rs = rules_from("version: 1\nrules:\n  - id: BAD\n    why: w\n    when: {fuzzer: HappyPath}\n")
        is_fp, rule_id, violation = R.classify_record(rs, rec(response_code=503), allow_5xx=True)
        self.assertTrue(is_fp)
        self.assertEqual(rule_id, "BAD")
        self.assertIsNone(violation)


class TestLoadValidation(unittest.TestCase):
    def test_duplicate_rule_id_rejected(self):
        with self.assertRaises(R.RuleError) as ctx:
            rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {response_code: 400}\n"
                       "  - id: A\n    why: w\n    when: {response_code: 401}\n")
        self.assertIn("A", str(ctx.exception))

    def test_unknown_field_rejected(self):
        with self.assertRaises(R.RuleError) as ctx:
            rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {bogus_field: 1}\n")
        self.assertIn("bogus_field", str(ctx.exception))

    def test_unknown_operator_rejected(self):
        with self.assertRaises(R.RuleError) as ctx:
            rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {path: {startswith: /a}}\n")
        self.assertIn("startswith", str(ctx.exception))

    def test_missing_why_rejected(self):
        with self.assertRaises(R.RuleError):
            rules_from("version: 1\nrules:\n  - id: A\n    when: {response_code: 400}\n")

    def test_rule_needs_when_or_any_of(self):
        with self.assertRaises(R.RuleError):
            rules_from("version: 1\nrules:\n  - id: A\n    why: w\n")

    def test_order_index_preserved(self):
        rs = rules_from("version: 1\nrules:\n  - id: A\n    why: w\n    when: {response_code: 400}\n"
                        "  - id: B\n    why: w\n    when: {response_code: 401}\n")
        self.assertEqual([r.id for r in rs], ["A", "B"])
        self.assertEqual([r.order_index for r in rs], [0, 1])

    def test_empty_rules_file_is_valid(self):
        self.assertEqual(rules_from("version: 1\nrules: []\n"), [])


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_rules.py -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'catslib.rules'`

- [ ] **Step 3: Implement `rules.py`**

```python
"""Declarative false-positive rules: loading, validation, and matching."""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml

SUPPORTED_VERSIONS = {1}

SCALAR_FIELDS = frozenset({
    "result", "response_code", "fuzzer", "path", "contract_path", "method", "url",
    "scenario", "result_reason", "result_details", "response_body",
    "response_content_type", "request_body", "any_text",
})
PREFIX_FIELDS = ("json_body.", "request_header.")
FIELDS = SCALAR_FIELDS | frozenset({"json_body", "request_headers"})

CASE_SENSITIVE_OPS = frozenset({"equals", "in", "not_equals"})
CASE_INSENSITIVE_OPS = frozenset({
    "contains", "contains_any", "contains_all", "starts_with", "starts_with_any",
    "ends_with", "matches",
})
OPERATORS = CASE_SENSITIVE_OPS | CASE_INSENSITIVE_OPS | frozenset({"exists"})

RULE_KEYS = frozenset({"id", "why", "when", "any_of", "enabled", "tags"})


class RuleError(Exception):
    """Raised with an actionable message when a rules file is invalid."""


@dataclass(frozen=True)
class Rule:
    id: str
    why: str
    when: dict[str, Any] | None
    any_of: list[dict[str, Any]] | None
    enabled: bool
    tags: list[str] = field(default_factory=list)
    order_index: int = 0


def _validate_field(name: str, where: str) -> None:
    if name in SCALAR_FIELDS or name in FIELDS:
        return
    if any(name.startswith(p) and len(name) > len(p) for p in PREFIX_FIELDS):
        return
    raise RuleError(f"{where}: unknown field {name!r}")


def _validate_condition(name: str, spec: Any, where: str) -> None:
    _validate_field(name, where)
    if isinstance(spec, dict):
        unknown = sorted(set(spec) - OPERATORS)
        if unknown:
            raise RuleError(f"{where}: unknown operator(s) {', '.join(unknown)} on field {name!r}")
        if not spec:
            raise RuleError(f"{where}: empty operator mapping on field {name!r}")


def load_rules(path: Path) -> list[Rule]:
    try:
        raw = yaml.safe_load(path.read_text()) or {}
    except FileNotFoundError as exc:
        raise RuleError(f"rules file not found: {path}") from exc
    except yaml.YAMLError as exc:
        raise RuleError(f"{path}: invalid YAML: {exc}") from exc
    if not isinstance(raw, dict):
        raise RuleError(f"{path}: top level must be a mapping with 'version' and 'rules'")

    version = raw.get("version", 1)
    if version not in SUPPORTED_VERSIONS:
        raise RuleError(f"{path}: unsupported version {version!r}")

    entries = raw.get("rules")
    if entries is None:
        raise RuleError(f"{path}: missing 'rules' list")
    if not isinstance(entries, list):
        raise RuleError(f"{path}: 'rules' must be a list")

    rules: list[Rule] = []
    seen: set[str] = set()
    for index, entry in enumerate(entries):
        where = f"{path}: rule #{index + 1}"
        if not isinstance(entry, dict):
            raise RuleError(f"{where}: must be a mapping")
        unknown = sorted(set(entry) - RULE_KEYS)
        if unknown:
            raise RuleError(f"{where}: unknown key(s) {', '.join(unknown)}")
        rule_id = entry.get("id")
        if not rule_id:
            raise RuleError(f"{where}: missing 'id'")
        if rule_id in seen:
            raise RuleError(f"{path}: duplicate rule id {rule_id!r}")
        seen.add(rule_id)
        if not entry.get("why"):
            raise RuleError(f"{where} ({rule_id}): missing 'why' — every rule must justify itself")

        when = entry.get("when")
        any_of = entry.get("any_of")
        if when is None and any_of is None:
            raise RuleError(f"{where} ({rule_id}): needs 'when' or 'any_of'")
        for block in ([when] if when is not None else []) + list(any_of or []):
            if not isinstance(block, dict) or not block:
                raise RuleError(f"{where} ({rule_id}): condition blocks must be non-empty mappings")
            for name, spec in block.items():
                _validate_condition(name, spec, f"{where} ({rule_id})")

        rules.append(Rule(
            id=rule_id,
            why=entry["why"],
            when=when,
            any_of=any_of,
            enabled=bool(entry.get("enabled", True)),
            tags=list(entry.get("tags") or []),
            order_index=index,
        ))
    return rules


_MISSING = object()


def field_value(record: dict[str, Any], name: str) -> Any:
    """Resolve a vocabulary field (including virtual and dotted fields) from a record."""
    if name == "any_text":
        return f"{record.get('result_reason') or ''} {record.get('result_details') or ''}"
    if name.startswith("json_body."):
        cursor: Any = record.get("json_body")
        for part in name[len("json_body."):].split("."):
            if not isinstance(cursor, dict) or part not in cursor:
                return _MISSING
            cursor = cursor[part]
        return cursor
    if name.startswith("request_header."):
        headers = record.get("request_headers") or {}
        return headers.get(name[len("request_header."):].lower(), _MISSING)
    return record.get(name, _MISSING)


def _as_text(value: Any) -> str:
    if value is _MISSING or value is None:
        return ""
    return value if isinstance(value, str) else str(value)


def _apply(op: str, actual: Any, expected: Any) -> bool:
    if op == "exists":
        present = actual is not _MISSING and actual is not None
        return present is bool(expected)
    if op == "equals":
        return actual == expected
    if op == "not_equals":
        return actual != expected
    if op == "in":
        return actual in expected

    text = _as_text(actual).lower()
    if op == "contains":
        return str(expected).lower() in text
    if op == "contains_any":
        return any(str(item).lower() in text for item in expected)
    if op == "contains_all":
        return all(str(item).lower() in text for item in expected)
    if op == "starts_with":
        return text.startswith(str(expected).lower())
    if op == "starts_with_any":
        return any(text.startswith(str(item).lower()) for item in expected)
    if op == "ends_with":
        return text.endswith(str(expected).lower())
    if op == "matches":
        return re.search(str(expected), text, re.IGNORECASE) is not None
    raise RuleError(f"unhandled operator {op!r}")


def _match_block(block: dict[str, Any], record: dict[str, Any]) -> bool:
    for name, spec in block.items():
        actual = field_value(record, name)
        if isinstance(spec, dict):
            if not all(_apply(op, actual, expected) for op, expected in spec.items()):
                return False
        elif actual != spec:
            return False
    return True


def match_rule(rule: Rule, record: dict[str, Any]) -> bool:
    if rule.when is not None and not _match_block(rule.when, record):
        return False
    if rule.any_of is not None and not any(_match_block(b, record) for b in rule.any_of):
        return False
    return True


def classify_record(
    rules: list[Rule], record: dict[str, Any], *, allow_5xx: bool
) -> tuple[bool, str | None, str | None]:
    """Return (is_false_positive, rule_id, violation_rule_id).

    Only 'error' and 'warn' results are classified. A rule matching a 5xx response is
    refused unless allow_5xx is set; the rule id is returned as violation_rule_id so the
    caller can report it.
    """
    if record.get("result") not in ("error", "warn"):
        return (False, None, None)
    for rule in rules:
        if not rule.enabled:
            continue
        if match_rule(rule, record):
            code = record.get("response_code") or 0
            if 500 <= int(code) < 600 and not allow_5xx:
                return (False, None, rule.id)
            return (True, rule.id, None)
    return (False, None, None)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_rules.py -v`
Expected: PASS (28 tests)

- [ ] **Step 5: Commit**

```bash
cd ~/Projects/skills
git add cats/scripts/catslib/rules.py tests/test_cats_rules.py
git commit -m "feat(cats): declarative false-positive rule engine"
```

---

## Task 3: SQLite schema and report parser

**Files:**
- Create: `~/Projects/skills/cats/scripts/catslib/schema.sql`
- Create: `~/Projects/skills/cats/scripts/catslib/parse.py`
- Test: `~/Projects/skills/tests/test_cats_parse.py`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `create_schema(conn: sqlite3.Connection) -> None`
  - `record_from_json(data: dict) -> dict` — the normalized record shape from Task 2
  - `parse_report(report_dir: Path, db_path: Path, run_meta: dict, *, batch_size: int = 500) -> ParseStats`
  - `ParseStats` dataclass: `processed: int, skipped: int, errors: int`
  - `extract_test_number(test_id: str) -> int`

**Schema:** today's 6 tables / 5 views / 18 indexes verbatim, plus `run_meta`, `fp_rules`, `true_positives_view`, and the two body columns.

- [ ] **Step 1: Write `schema.sql`**

Copy the DDL from `/Users/efitz/Projects/tmi/scripts/parse_cats_results.py` (`SCHEMA_SQL` at line 39, `INDEX_SQL` at line 133, `VIEWS_SQL` at line 163) verbatim, then apply exactly these additions:

```sql
-- Added: provenance for this run (exactly one row)
CREATE TABLE IF NOT EXISTS run_meta (
    run_id TEXT PRIMARY KEY,
    started_at TEXT,
    finished_at TEXT,
    identity TEXT,
    spec_path TEXT,
    spec_sha256 TEXT,
    rules_sha256 TEXT,
    git_sha TEXT,
    cats_version TEXT,
    cats_args TEXT,
    server TEXT,
    tool_version TEXT
);

-- Added: the rule set that produced this DB's classification
CREATE TABLE IF NOT EXISTS fp_rules (
    rule_id TEXT PRIMARY KEY,
    why TEXT NOT NULL,
    order_index INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT 1,
    match_count INTEGER NOT NULL DEFAULT 0
);
```

Add these two columns to the existing `requests` / `responses` table definitions:

```sql
-- in requests:   request_body TEXT
-- in responses:  response_body TEXT
```

And append this view to `VIEWS_SQL`:

```sql
CREATE VIEW IF NOT EXISTS true_positives_view AS
SELECT * FROM test_results_view
WHERE is_false_positive = 0 AND result IN ('error', 'warn');
```

- [ ] **Step 2: Write the failing tests**

`tests/test_cats_parse.py`:

```python
import json
import sqlite3
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cats" / "scripts"))

from catslib import parse as P


def cats_json(**over):
    data = {
        "testId": "Test 1", "traceId": "t-1", "fuzzer": "HappyPath",
        "path": "/things", "contractPath": "/things", "fullRequestPath": "/things",
        "scenario": "happy", "expectedResult": "Should return 200",
        "result": "error", "resultReason": "Unexpected Response Code: 400",
        "resultDetails": "details", "server": "http://h",
        "request": {"httpMethod": "POST", "url": "http://h/things",
                     "timestamp": "2026-07-26T00:00:00Z", "payload": '{"a":1}',
                     "headers": [{"key": "Accept", "value": "application/json"}]},
        "response": {"httpMethod": "POST", "responseCode": 400, "responseTimeInMs": 12,
                      "numberOfWordsInResponse": 3, "numberOfLinesInResponse": 1,
                      "contentLengthInBytes": 42, "responseContentType": "application/json",
                      "jsonBody": {"error": "bad"},
                      "headers": [{"key": "Content-Type", "value": "application/json"}]},
    }
    data.update(over)
    return data


class TestRecordNormalization(unittest.TestCase):
    def test_maps_cats_json_to_record(self):
        r = P.record_from_json(cats_json())
        self.assertEqual(r["result"], "error")
        self.assertEqual(r["response_code"], 400)
        self.assertEqual(r["method"], "POST")
        self.assertEqual(r["path"], "/things")
        self.assertEqual(r["json_body"], {"error": "bad"})
        self.assertEqual(json.loads(r["response_body"]), {"error": "bad"})
        self.assertEqual(r["request_body"], '{"a":1}')
        self.assertEqual(r["request_headers"], {"accept": "application/json"})

    def test_null_fields_become_empty_strings(self):
        r = P.record_from_json(cats_json(resultReason=None, resultDetails=None))
        self.assertEqual(r["result_reason"], "")
        self.assertEqual(r["result_details"], "")

    def test_absent_json_body_yields_empty_response_body(self):
        data = cats_json()
        del data["response"]["jsonBody"]
        r = P.record_from_json(data)
        self.assertIsNone(r["json_body"])
        self.assertEqual(r["response_body"], "")

    def test_null_headers_tolerated(self):
        data = cats_json()
        data["request"]["headers"] = None
        data["response"]["headers"] = None
        self.assertEqual(P.record_from_json(data)["request_headers"], {})


class TestExtractTestNumber(unittest.TestCase):
    def test_parses_spaced_and_unspaced_forms(self):
        self.assertEqual(P.extract_test_number("Test 42"), 42)
        self.assertEqual(P.extract_test_number("Test42"), 42)

    def test_unparseable_returns_zero(self):
        self.assertEqual(P.extract_test_number("weird"), 0)


class TestParseReport(unittest.TestCase):
    def _report(self, files):
        d = Path(tempfile.mkdtemp())
        for name, data in files.items():
            (d / name).write_text(json.dumps(data))
        return d

    def test_parses_files_into_db(self):
        report = self._report({
            "Test1.json": cats_json(),
            "Test2.json": cats_json(testId="Test 2", result="success",
                                    response={**cats_json()["response"], "responseCode": 200}),
        })
        db = Path(tempfile.mkdtemp()) / "r.db"
        stats = P.parse_report(report, db, {"run_id": "R1", "server": "http://h"})
        self.assertEqual(stats.processed, 2)
        conn = sqlite3.connect(db)
        self.assertEqual(conn.execute("SELECT COUNT(*) FROM tests").fetchone()[0], 2)
        self.assertEqual(conn.execute("SELECT run_id FROM run_meta").fetchone()[0], "R1")

    def test_bodies_stored_only_for_error_and_warn(self):
        report = self._report({
            "Test1.json": cats_json(),                       # error -> body stored
            "Test2.json": cats_json(testId="Test 2", result="success"),
        })
        db = Path(tempfile.mkdtemp()) / "r.db"
        P.parse_report(report, db, {"run_id": "R1"})
        conn = sqlite3.connect(db)
        rows = dict(conn.execute(
            "SELECT t.test_id, r.response_body FROM tests t JOIN responses r ON r.test_id = t.id"
        ).fetchall())
        self.assertIn("bad", rows["Test 1"])
        self.assertIn(rows["Test 2"], (None, ""))

    def test_headers_persisted_in_order(self):
        report = self._report({"Test1.json": cats_json()})
        db = Path(tempfile.mkdtemp()) / "r.db"
        P.parse_report(report, db, {"run_id": "R1"})
        conn = sqlite3.connect(db)
        self.assertEqual(
            conn.execute("SELECT header_key, header_order FROM request_headers").fetchall(),
            [("Accept", 0)],
        )

    def test_malformed_file_is_skipped_not_fatal(self):
        report = self._report({"Test1.json": cats_json()})
        (report / "Test2.json").write_text("{not json")
        db = Path(tempfile.mkdtemp()) / "r.db"
        stats = P.parse_report(report, db, {"run_id": "R1"})
        self.assertEqual(stats.processed, 1)
        self.assertEqual(stats.skipped, 1)

    def test_views_exist(self):
        report = self._report({"Test1.json": cats_json()})
        db = Path(tempfile.mkdtemp()) / "r.db"
        P.parse_report(report, db, {"run_id": "R1"})
        conn = sqlite3.connect(db)
        names = {r[0] for r in conn.execute(
            "SELECT name FROM sqlite_master WHERE type='view'").fetchall()}
        self.assertIn("true_positives_view", names)
        self.assertIn("test_results_filtered_view", names)


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_parse.py -v`
Expected: FAIL — no module `catslib.parse`

- [ ] **Step 4: Implement `parse.py`**

Port the lookup-cache and batching structure from the legacy parser (`get_or_create_*` at lines 360–445, `process_directory` at 1568), dropping all false-positive logic. Key implementation points:

```python
"""Parse a CATS report directory into a normalized SQLite database."""

from __future__ import annotations

import json
import re
import sqlite3
from dataclasses import dataclass
from pathlib import Path
from typing import Any

SCHEMA_PATH = Path(__file__).with_name("schema.sql")
_TEST_NUM = re.compile(r"(\d+)")


@dataclass
class ParseStats:
    processed: int = 0
    skipped: int = 0
    errors: int = 0


def create_schema(conn: sqlite3.Connection) -> None:
    conn.executescript(SCHEMA_PATH.read_text())


def extract_test_number(test_id: str) -> int:
    match = _TEST_NUM.search(test_id or "")
    return int(match.group(1)) if match else 0


def record_from_json(data: dict[str, Any]) -> dict[str, Any]:
    request = data.get("request") or {}
    response = data.get("response") or {}
    json_body = response.get("jsonBody")
    headers = {
        (h.get("key") or "").lower(): h.get("value") or ""
        for h in (request.get("headers") or [])
    }
    return {
        "result": (data.get("result") or "").lower(),
        "response_code": int(response.get("responseCode") or 0),
        "fuzzer": data.get("fuzzer") or "",
        "path": data.get("path") or "",
        "contract_path": data.get("contractPath") or "",
        "method": request.get("httpMethod") or "",
        "url": request.get("url") or "",
        "scenario": data.get("scenario") or "",
        "result_reason": data.get("resultReason") or "",
        "result_details": data.get("resultDetails") or "",
        "response_body": json.dumps(json_body) if json_body is not None else "",
        "response_content_type": response.get("responseContentType") or "",
        "request_body": request.get("payload") or "",
        "json_body": json_body,
        "request_headers": headers,
    }
```

`parse_report` then: deletes any pre-existing `db_path`, opens a connection with
`PRAGMA journal_mode=WAL` and `PRAGMA synchronous=OFF` (bulk load), calls `create_schema`,
inserts the `run_meta` row from the supplied dict (missing keys → `None`), then iterates
`sorted(report_dir.glob("Test*.json"), key=lambda p: extract_test_number(p.stem))` in
batches of `batch_size` inside transactions. Per file: `json.loads` (on `JSONDecodeError` or
missing required key, increment `skipped` and continue), build the record, insert into
`tests` (with `is_false_positive = 0`, `fp_rule = NULL` — classification is Task 4's job),
then `requests`, `responses`, and both header tables. Store `request_body` / `response_body`
only when `record["result"] in ("error", "warn")`, otherwise `NULL`. Lookup tables
(`result_types`, `fuzzers`, `servers`, `paths`, `http_methods`) use in-memory dict caches
seeded by a `SELECT` at open, exactly as the legacy parser does.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_parse.py -v`
Expected: PASS (12 tests)

- [ ] **Step 6: Verify against the real corpus**

```bash
cd ~/Projects/skills && uv run --with pyyaml python -c "
from pathlib import Path
import sys; sys.path.insert(0, 'cats/scripts')
from catslib.parse import parse_report
s = parse_report(Path('/Users/efitz/Projects/tmi/test/outputs/cats/report'),
                 Path('/tmp/corpus-parse.db'), {'run_id': 'corpus'})
print(s)"
```

Expected: `processed=121940` (or processed+skipped == 121940 with skipped explained), no exceptions.

- [ ] **Step 7: Commit**

```bash
cd ~/Projects/skills
git add cats/scripts/catslib/schema.sql cats/scripts/catslib/parse.py tests/test_cats_parse.py
git commit -m "feat(cats): sqlite schema and report parser"
```

---

## Task 4: Classifier with reclassification deltas

**Files:**
- Create: `~/Projects/skills/cats/scripts/catslib/classify.py`
- Test: `~/Projects/skills/tests/test_cats_classify.py`

**Interfaces:**
- Consumes: `catslib.rules.load_rules/classify_record/Rule`, `catslib.parse.create_schema`.
- Produces:
  - `record_from_db(conn, test_row_id: int) -> dict` — normalized record rebuilt from the DB
  - `classify_db(db_path: Path, rules: list[Rule], *, allow_5xx: bool) -> ClassifyResult`
  - `ClassifyResult` dataclass: `total: int, flagged: int, by_rule: dict[str, int], violations: list[tuple[str, str]], newly_suppressed: list[str], newly_surfaced: list[str]` (the last two are test_ids, empty on a first pass)

Classification must be idempotent and re-runnable: it recomputes every `error`/`warn` row from scratch, so removing a rule surfaces findings again.

- [ ] **Step 1: Write the failing tests**

`tests/test_cats_classify.py`:

```python
import json
import sqlite3
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cats" / "scripts"))

from catslib import classify as C
from catslib import parse as P
from catslib import rules as R


def cats_json(**over):
    data = {
        "testId": "Test 1", "traceId": "t-1", "fuzzer": "HappyPath",
        "path": "/things", "contractPath": "/things", "scenario": "s",
        "expectedResult": "200", "result": "error",
        "resultReason": "Unexpected Response Code: 400", "resultDetails": "",
        "server": "http://h",
        "request": {"httpMethod": "POST", "url": "http://h/things",
                     "timestamp": "", "payload": "", "headers": []},
        "response": {"httpMethod": "POST", "responseCode": 400, "responseTimeInMs": 1,
                      "numberOfWordsInResponse": 1, "numberOfLinesInResponse": 1,
                      "contentLengthInBytes": 1, "responseContentType": "application/json",
                      "jsonBody": {"error_description": "bad enum_values"}, "headers": []},
    }
    data.update(over)
    return data


def build_db(tests):
    report = Path(tempfile.mkdtemp())
    for i, data in enumerate(tests, 1):
        (report / f"Test{i}.json").write_text(json.dumps(data))
    db = Path(tempfile.mkdtemp()) / "r.db"
    P.parse_report(report, db, {"run_id": "R1"})
    return db


def write_rules(text):
    with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as fh:
        fh.write(text)
    return R.load_rules(Path(fh.name))


ONE_RULE = "version: 1\nrules:\n  - id: VALIDATION_400\n    why: correct rejection\n    when: {response_code: 400}\n"


class TestClassifyDb(unittest.TestCase):
    def test_flags_matching_rows(self):
        db = build_db([cats_json()])
        result = C.classify_db(db, write_rules(ONE_RULE), allow_5xx=False)
        self.assertEqual(result.flagged, 1)
        self.assertEqual(result.by_rule, {"VALIDATION_400": 1})
        conn = sqlite3.connect(db)
        row = conn.execute("SELECT is_false_positive, fp_rule FROM tests").fetchone()
        self.assertEqual(row, (1, "VALIDATION_400"))

    def test_success_rows_never_flagged(self):
        db = build_db([cats_json(result="success")])
        result = C.classify_db(db, write_rules(ONE_RULE), allow_5xx=False)
        self.assertEqual(result.flagged, 0)

    def test_rule_matching_on_json_body_path(self):
        rules = write_rules("version: 1\nrules:\n  - id: ADDON\n    why: w\n"
                            "    when: {json_body.error_description: {contains: enum_values}}\n")
        db = build_db([cats_json()])
        self.assertEqual(C.classify_db(db, rules, allow_5xx=False).flagged, 1)

    def test_rules_table_populated_with_counts(self):
        db = build_db([cats_json()])
        C.classify_db(db, write_rules(ONE_RULE), allow_5xx=False)
        conn = sqlite3.connect(db)
        self.assertEqual(
            conn.execute("SELECT rule_id, why, match_count FROM fp_rules").fetchall(),
            [("VALIDATION_400", "correct rejection", 1)],
        )

    def test_zero_match_rule_recorded_for_staleness_detection(self):
        rules = write_rules(ONE_RULE + "  - id: NEVER\n    why: w\n    when: {response_code: 418}\n")
        db = build_db([cats_json()])
        C.classify_db(db, rules, allow_5xx=False)
        conn = sqlite3.connect(db)
        counts = dict(conn.execute("SELECT rule_id, match_count FROM fp_rules").fetchall())
        self.assertEqual(counts["NEVER"], 0)

    def test_5xx_violation_reported_and_not_suppressed(self):
        resp = {**cats_json()["response"], "responseCode": 500}
        db = build_db([cats_json(response=resp)])
        rules = write_rules("version: 1\nrules:\n  - id: TOO_BROAD\n    why: w\n    when: {fuzzer: HappyPath}\n")
        result = C.classify_db(db, rules, allow_5xx=False)
        self.assertEqual(result.flagged, 0)
        self.assertEqual(result.violations, [("TOO_BROAD", "Test 1")])

    def test_reclassify_reports_newly_suppressed(self):
        db = build_db([cats_json()])
        C.classify_db(db, [], allow_5xx=False)
        result = C.classify_db(db, write_rules(ONE_RULE), allow_5xx=False)
        self.assertEqual(result.newly_suppressed, ["Test 1"])
        self.assertEqual(result.newly_surfaced, [])

    def test_reclassify_reports_newly_surfaced_when_rule_removed(self):
        db = build_db([cats_json()])
        C.classify_db(db, write_rules(ONE_RULE), allow_5xx=False)
        result = C.classify_db(db, [], allow_5xx=False)
        self.assertEqual(result.newly_surfaced, ["Test 1"])
        self.assertEqual(result.newly_suppressed, [])

    def test_classification_is_idempotent(self):
        db = build_db([cats_json()])
        rules = write_rules(ONE_RULE)
        first = C.classify_db(db, rules, allow_5xx=False)
        second = C.classify_db(db, rules, allow_5xx=False)
        self.assertEqual(first.flagged, second.flagged)
        self.assertEqual(second.newly_suppressed, [])
        self.assertEqual(second.newly_surfaced, [])


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_classify.py -v`
Expected: FAIL — no module `catslib.classify`

- [ ] **Step 3: Implement `classify.py`**

```python
"""Apply declarative rules to a parsed CATS database."""

from __future__ import annotations

import json
import sqlite3
from dataclasses import dataclass, field
from pathlib import Path

from .rules import Rule, classify_record

SELECT_CANDIDATES = """
SELECT t.id, t.test_id, t.is_false_positive, rt.name AS result, t.result_reason,
       t.result_details, t.scenario, f.name AS fuzzer, p.path, p.contract_path,
       req.url, m.method, req.request_body, resp.response_code,
       resp.response_content_type, resp.response_body
FROM tests t
JOIN result_types rt ON t.result_type_id = rt.id
JOIN fuzzers f ON t.fuzzer_id = f.id
JOIN paths p ON t.path_id = p.id
JOIN requests req ON req.test_id = t.id
JOIN http_methods m ON req.http_method_id = m.id
JOIN responses resp ON resp.test_id = t.id
WHERE rt.name IN ('error', 'warn')
"""


@dataclass
class ClassifyResult:
    total: int = 0
    flagged: int = 0
    by_rule: dict[str, int] = field(default_factory=dict)
    violations: list[tuple[str, str]] = field(default_factory=list)
    newly_suppressed: list[str] = field(default_factory=list)
    newly_surfaced: list[str] = field(default_factory=list)


def _headers(conn: sqlite3.Connection, row_id: int) -> dict[str, str]:
    rows = conn.execute(
        "SELECT header_key, header_value FROM request_headers rh "
        "JOIN requests r ON rh.request_id = r.id WHERE r.test_id = ?",
        (row_id,),
    ).fetchall()
    return {(k or "").lower(): v or "" for k, v in rows}


def _record(conn: sqlite3.Connection, row: sqlite3.Row) -> dict:
    body = row["response_body"] or ""
    try:
        json_body = json.loads(body) if body else None
    except json.JSONDecodeError:
        json_body = None
    return {
        "result": row["result"],
        "response_code": row["response_code"],
        "fuzzer": row["fuzzer"],
        "path": row["path"],
        "contract_path": row["contract_path"] or "",
        "method": row["method"],
        "url": row["url"] or "",
        "scenario": row["scenario"] or "",
        "result_reason": row["result_reason"] or "",
        "result_details": row["result_details"] or "",
        "response_body": body,
        "response_content_type": row["response_content_type"] or "",
        "request_body": row["request_body"] or "",
        "json_body": json_body,
        "request_headers": _headers(conn, row["id"]),
    }


def classify_db(db_path: Path, rules: list[Rule], *, allow_5xx: bool) -> ClassifyResult:
    conn = sqlite3.connect(db_path)
    conn.row_factory = sqlite3.Row
    result = ClassifyResult()
    counts = {rule.id: 0 for rule in rules}
    updates: list[tuple[int, str | None, int]] = []
    try:
        for row in conn.execute(SELECT_CANDIDATES):
            result.total += 1
            record = _record(conn, row)
            is_fp, rule_id, violation = classify_record(rules, record, allow_5xx=allow_5xx)
            if violation:
                result.violations.append((violation, row["test_id"]))
            was_fp = bool(row["is_false_positive"])
            if is_fp and not was_fp:
                result.newly_suppressed.append(row["test_id"])
            elif was_fp and not is_fp:
                result.newly_surfaced.append(row["test_id"])
            if is_fp:
                result.flagged += 1
                counts[rule_id] = counts.get(rule_id, 0) + 1
            updates.append((1 if is_fp else 0, rule_id, row["id"]))

        with conn:
            conn.executemany(
                "UPDATE tests SET is_false_positive = ?, fp_rule = ? WHERE id = ?", updates
            )
            conn.execute("DELETE FROM fp_rules")
            conn.executemany(
                "INSERT INTO fp_rules (rule_id, why, order_index, enabled, match_count) "
                "VALUES (?, ?, ?, ?, ?)",
                [(r.id, r.why, r.order_index, 1 if r.enabled else 0, counts.get(r.id, 0))
                 for r in rules],
            )
        result.by_rule = {k: v for k, v in counts.items() if v}
    finally:
        conn.close()
    return result
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_classify.py -v`
Expected: PASS (9 tests)

- [ ] **Step 5: Commit**

```bash
cd ~/Projects/skills
git add cats/scripts/catslib/classify.py tests/test_cats_classify.py
git commit -m "feat(cats): rule classifier with reclassification deltas"
```

---

## Task 5: Runner — hooks, token resolution, CATS invocation

**Files:**
- Create: `~/Projects/skills/cats/scripts/catslib/runner.py`
- Test: `~/Projects/skills/tests/test_cats_runner.py`

**Interfaces:**
- Consumes: `catslib.config.Config/Identity/ConfigError`, `catslib.parse.parse_report`, `catslib.classify.classify_db`, `catslib.rules.load_rules`.
**On `shell=True` (deliberate — do not "fix"):** hooks and `token_cmd` are shell command
strings authored by the repo owner in their own gitignored `.local/cats/config.yaml`, the
same trust model as a Makefile recipe or a git hook. They need shell semantics (`&&`, pipes,
env expansion, quoting), so they are executed with `shell=True` by design. There is no
untrusted input path: nothing from the fuzz results, the OpenAPI spec, or the network ever
reaches a hook string. Converting these to list-form `subprocess.run([...])` would break the
documented contract. By contrast, the CATS invocation itself **is** built as an argv list
(`build_cats_argv`) and must stay that way.

- Produces:
  - `HookError(Exception)`, `PreflightError(Exception)`
  - `redact(text: str, token: str) -> str`
  - `run_hook(name: str, command: str, cwd: Path, env: dict[str, str]) -> None`
  - `resolve_token(identity: Identity, cwd: Path, env: dict[str, str]) -> str`
  - `write_headers_file(directory: Path, header: str, value: str) -> Path`
  - `build_cats_argv(config, *, headers_file: Path, report_dir: Path, path_filter: str | None, rate: int | None, blackbox: bool) -> list[str]`
  - `run_id_for(now: datetime) -> str` → `"20260726T220200Z"`
  - `execute(config, *, identity_name=None, path_filter=None, rate=None, blackbox=False, skip_seed=False, skip_parse=False, now=None) -> RunResult`
  - `RunResult` dataclass: `run_id, db_path, report_dir, cats_exit_code, parse_stats, classify_result`

- [ ] **Step 1: Write the failing tests**

`tests/test_cats_runner.py`:

```python
import os
import stat
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cats" / "scripts"))

from catslib import config as cfg
from catslib import runner as run

CONFIG = """
version: 1
spec: openapi.json
server: http://localhost:8080
results_dir: results
false_positives: fp.yaml
identities:
  admin: {token_cmd: "printf secret-token"}
  other: {token_cmd: "printf other-token"}
default_identity: admin
cats:
  max_requests_per_minute: 500
  skip_fuzzers: [DuplicateHeaders, EnumCaseVariantFields]
  skip_field_format: [uuid]
  skip_field: [offset]
  skip_fuzzers_for_extension:
    - {extension: x-public-endpoint, value: "true", fuzzers: [BypassAuthentication]}
  extra_args: ["--printExecutionStatistics"]
"""


def make_config(body=CONFIG):
    root = Path(tempfile.mkdtemp())
    (root / ".local" / "cats").mkdir(parents=True)
    p = root / ".local" / "cats" / "config.yaml"
    p.write_text(body)
    (root / "openapi.json").write_text("{}")
    (root / "fp.yaml").write_text("version: 1\nrules: []\n")
    return cfg.load_config(p)


class TestRedact(unittest.TestCase):
    def test_replaces_every_occurrence(self):
        self.assertEqual(run.redact("a tok b tok", "tok"), "a [REDACTED] b [REDACTED]")

    def test_empty_token_is_noop(self):
        self.assertEqual(run.redact("abc", ""), "abc")


class TestResolveToken(unittest.TestCase):
    def test_captures_stdout_trimmed(self):
        c = make_config()
        self.assertEqual(run.resolve_token(c.identities["admin"], c.repo_root, {}), "secret-token")

    def test_empty_token_raises(self):
        c = make_config(CONFIG.replace('printf secret-token', 'printf ""'))
        with self.assertRaises(run.HookError):
            run.resolve_token(c.identities["admin"], c.repo_root, {})

    def test_failing_command_raises_with_command_in_message(self):
        c = make_config(CONFIG.replace('printf secret-token', 'exit 3'))
        with self.assertRaises(run.HookError) as ctx:
            run.resolve_token(c.identities["admin"], c.repo_root, {})
        self.assertIn("exit 3", str(ctx.exception))


class TestRunHook(unittest.TestCase):
    def test_success_is_silent(self):
        run.run_hook("seed", "true", Path("."), {})

    def test_nonzero_exit_raises_hook_error(self):
        with self.assertRaises(run.HookError) as ctx:
            run.run_hook("seed", "exit 7", Path("."), {})
        self.assertIn("seed", str(ctx.exception))

    def test_env_is_passed_through(self):
        with tempfile.TemporaryDirectory() as d:
            out = Path(d) / "out"
            run.run_hook("seed", f'printf "$CATS_RUN_ID" > {out}', Path(d), {"CATS_RUN_ID": "R9"})
            self.assertEqual(out.read_text(), "R9")


class TestHeadersFile(unittest.TestCase):
    def test_written_with_owner_only_permissions(self):
        with tempfile.TemporaryDirectory() as d:
            p = run.write_headers_file(Path(d), "Authorization", "Bearer tok")
            self.assertEqual(stat.S_IMODE(p.stat().st_mode), 0o600)
            self.assertIn("Authorization: Bearer tok", p.read_text())
            self.assertIn("all:", p.read_text())


class TestBuildArgv(unittest.TestCase):
    def _argv(self, **kw):
        c = make_config()
        return run.build_cats_argv(
            c, headers_file=Path("/tmp/h.yml"), report_dir=Path("/tmp/rep"),
            path_filter=kw.get("path_filter"), rate=kw.get("rate"),
            blackbox=kw.get("blackbox", False))

    def test_core_flags_present(self):
        argv = self._argv()
        self.assertEqual(argv[0], "cats")
        self.assertIn("--server=http://localhost:8080", argv)
        self.assertIn("--headers=/tmp/h.yml", argv)
        self.assertIn("--output=/tmp/rep", argv)
        self.assertIn("--maxRequestsPerMinute=500", argv)

    def test_token_never_appears_in_argv(self):
        self.assertNotIn("secret-token", " ".join(self._argv()))

    def test_skip_options_rendered(self):
        argv = " ".join(self._argv())
        self.assertIn("--skipFuzzers=DuplicateHeaders,EnumCaseVariantFields", argv)
        self.assertIn("--skipFieldFormat=uuid", argv)
        self.assertIn("--skipField=offset", argv)
        self.assertIn("--skipFuzzersForExtension=x-public-endpoint=true:BypassAuthentication", argv)

    def test_extra_args_appended(self):
        self.assertIn("--printExecutionStatistics", self._argv())

    def test_path_filter_and_blackbox_optional(self):
        self.assertNotIn("-b", self._argv())
        argv = self._argv(path_filter="/things", blackbox=True)
        self.assertIn("--paths=/things", argv)
        self.assertIn("-b", argv)

    def test_rate_override_wins_over_config(self):
        self.assertIn("--maxRequestsPerMinute=99", self._argv(rate=99))


class TestRunId(unittest.TestCase):
    def test_format(self):
        self.assertEqual(
            run.run_id_for(datetime(2026, 7, 26, 22, 2, 0, tzinfo=timezone.utc)),
            "20260726T220200Z")


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_runner.py -v`
Expected: FAIL — no module `catslib.runner`

- [ ] **Step 3: Implement `runner.py`**

```python
"""Hook-driven CATS run pipeline."""

from __future__ import annotations

import hashlib
import os
import shutil
import subprocess
import tempfile
import urllib.error
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .classify import ClassifyResult, classify_db
from .config import Config, Identity
from .parse import ParseStats, parse_report
from .rules import load_rules

TOOL_VERSION = "0.1.0"


class HookError(Exception):
    """A configured hook or token command failed."""


class PreflightError(Exception):
    """The environment is not ready to fuzz."""


@dataclass
class RunResult:
    run_id: str
    db_path: Path
    report_dir: Path
    cats_exit_code: int
    parse_stats: ParseStats | None
    classify_result: ClassifyResult | None


def redact(text: str, token: str) -> str:
    return text.replace(token, "[REDACTED]") if token else text


def run_id_for(now: datetime) -> str:
    return now.astimezone(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def run_hook(name: str, command: str, cwd: Path, env: dict[str, str]) -> None:
    proc = subprocess.run(command, shell=True, cwd=str(cwd), env={**os.environ, **env})
    if proc.returncode != 0:
        raise HookError(f"{name} hook failed (exit {proc.returncode}): {command}")


def resolve_token(identity: Identity, cwd: Path, env: dict[str, str]) -> str:
    proc = subprocess.run(
        identity.token_cmd, shell=True, cwd=str(cwd), env={**os.environ, **env},
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        raise HookError(
            f"token_cmd for identity {identity.name!r} failed "
            f"(exit {proc.returncode}): {identity.token_cmd}"
        )
    token = (proc.stdout or "").strip()
    if not token:
        raise HookError(
            f"token_cmd for identity {identity.name!r} produced no token: {identity.token_cmd}"
        )
    return token


def write_headers_file(directory: Path, header: str, value: str) -> Path:
    """Write a CATS headers file readable only by the owner."""
    fd, name = tempfile.mkstemp(prefix="cats-headers-", suffix=".yml", dir=str(directory))
    os.close(fd)
    path = Path(name)
    path.chmod(0o600)
    path.write_text(f"all:\n  {header}: {value}\n")
    return path


def build_cats_argv(config: Config, *, headers_file: Path, report_dir: Path,
                    path_filter: str | None, rate: int | None, blackbox: bool) -> list[str]:
    opts = config.cats
    argv = [
        "cats",
        f"--contract={config.spec}",
        f"--server={config.server}",
        f"--headers={headers_file}",
        f"--output={report_dir}",
        f"--maxRequestsPerMinute={rate or opts.max_requests_per_minute}",
    ]
    if blackbox:
        argv.append("-b")
    if opts.http_methods:
        argv.append(f"-X={','.join(opts.http_methods)}")
    if opts.ref_data:
        argv.append(f"--refData={opts.ref_data}")
    for value in opts.skip_field_format:
        argv.append(f"--skipFieldFormat={value}")
    for value in opts.skip_field:
        argv.append(f"--skipField={value}")
    if opts.skip_fuzzers:
        argv.append(f"--skipFuzzers={','.join(opts.skip_fuzzers)}")
    for entry in opts.skip_fuzzers_for_extension:
        value = entry.get("value", "true")
        fuzzers = ",".join(entry["fuzzers"])
        argv.append(f"--skipFuzzersForExtension={entry['extension']}={value}:{fuzzers}")
    if path_filter:
        argv.append(f"--paths={path_filter}")
    argv.extend(opts.extra_args)
    return argv
```

`preflight(config)` checks, raising `PreflightError` with an actionable message: `config.spec`
exists; `shutil.which("cats")` is not None (message: install instructions — the CATS project
URL and `brew install cats`); `config.false_positives` loads via `load_rules`;
`urllib.request.urlopen(config.health_url, timeout=5)` succeeds (message: "server is not
running at <url>; start it first").

`execute(...)` orchestrates, with a `try/finally` that always deletes the headers file:

1. `run_id = run_id_for(now or datetime.now(timezone.utc))`
2. `report_dir = config.results_dir / f"report-{run_id}"`, `db_path = config.results_dir / f"cats-results-{run_id}.db"`; `mkdir(parents=True, exist_ok=True)`
3. `preflight(config)`
4. hook env: `CATS_SERVER, CATS_SPEC, CATS_RESULTS_DIR, CATS_REPORT_DIR, CATS_RUN_ID, CATS_IDENTITY` (all `str`)
5. `hooks.seed` unless `skip_seed`; then `hooks.pre_run`
6. `token = resolve_token(...)`; `headers_file = write_headers_file(config.results_dir, config.auth_header, config.auth_template.format(token=token))`
7. `argv = build_cats_argv(...)`; log `redact(" ".join(argv), token)`; `subprocess.run(argv)` → `cats_exit_code`
8. unless `skip_parse`: `parse_report(report_dir, db_path, run_meta)` where `run_meta` carries `run_id`, `started_at`, `finished_at`, `identity`, `spec_path`, `spec_sha256` (`hashlib.sha256(spec.read_bytes()).hexdigest()`), `rules_sha256`, `git_sha` (`git rev-parse HEAD`, `None` on failure), `cats_version` (`cats --version` first line, `None` on failure), `cats_args` (`redact(" ".join(argv), token)`), `server`, `tool_version`; then `classify_db(db_path, load_rules(config.false_positives), allow_5xx=config.allow_suppressing_5xx)`
9. update the `latest.db` symlink: `Path(config.results_dir / "latest.db")`, unlink if present, `symlink_to(db_path.name)`
10. `hooks.post_run` with `CATS_DB` and `CATS_EXIT_CODE` added — wrap in `try/except HookError` and warn rather than fail
11. if not `config.retain_raw_report` and parsing succeeded: `shutil.rmtree(report_dir, ignore_errors=True)`
12. return `RunResult`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_runner.py -v`
Expected: PASS (16 tests)

- [ ] **Step 5: Commit**

```bash
cd ~/Projects/skills
git add cats/scripts/catslib/runner.py tests/test_cats_runner.py
git commit -m "feat(cats): hook-driven run pipeline with secure token handling"
```

---

## Task 6: CLI entrypoint, `init`, and `doctor`

**Files:**
- Create: `~/Projects/skills/cats/scripts/cats_tool.py`
- Modify: `~/Projects/skills/tests/test_cats_config.py` (append `TestInitTemplate`)

**Interfaces:**
- Consumes: everything from Tasks 1–5.
- Produces: the CLI contract every skill depends on:
  - `cats_tool.py init [--spec P] [--server URL] [--health-url URL] [--results-dir P] [--rules P] [--non-interactive]`
  - `cats_tool.py run [--identity N] [--path P] [--rate N] [--blackbox] [--skip-seed] [--skip-parse]`
  - `cats_tool.py parse --report DIR [--db FILE]`
  - `cats_tool.py classify [--db FILE] [--dry-run]`
  - `cats_tool.py query [--db FILE] [--sql SQL] [--json]`
  - `cats_tool.py report [--db FILE] [--out FILE] [--open]`
  - `cats_tool.py doctor`
  - Also `catslib.config.render_init_config(...) -> str` and `INITIAL_RULES_YAML` (module constant in `config.py`).
  - `--db` accepts a path or the literal `latest` (default), resolving through `results_dir/latest.db`.

- [ ] **Step 1: Write the failing test for the init template**

Append to `tests/test_cats_config.py`:

```python
class TestInitTemplate(unittest.TestCase):
    def test_rendered_config_round_trips(self):
        text = cfg.render_init_config(
            spec="api/openapi.json", server="http://localhost:3000",
            health_url="http://localhost:3000/health", results_dir="test/results/cats",
            rules="test/cats/false-positives.yaml")
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / ".local" / "cats").mkdir(parents=True)
            p = root / ".local" / "cats" / "config.yaml"
            p.write_text(text)
            c = cfg.load_config(p)
        self.assertEqual(c.server, "http://localhost:3000")
        self.assertEqual(c.health_url, "http://localhost:3000/health")

    def test_template_documents_hooks(self):
        text = cfg.render_init_config(
            spec="s", server="http://h", health_url="http://h",
            results_dir="r", rules="f.yaml")
        for key in ("seed:", "pre_run:", "post_run:", "token_cmd:"):
            self.assertIn(key, text)

    def test_starter_rules_are_stack_agnostic(self):
        from catslib import rules as R
        with tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False) as fh:
            fh.write(cfg.INITIAL_RULES_YAML)
        loaded = R.load_rules(Path(fh.name))
        self.assertEqual([r.id for r in loaded], ["RATE_LIMIT_429", "CONNECTION_ERROR_999"])
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_config.py -k Init -v`
Expected: FAIL — `AttributeError: module 'catslib.config' has no attribute 'render_init_config'`

- [ ] **Step 3: Add the template to `config.py`**

```python
INITIAL_RULES_YAML = """# CATS false-positive rules.
#
# Rules are evaluated in file order; the FIRST match wins and its id is recorded
# on the test row. Only 'error' and 'warn' results are classified.
#
# Fields: result, response_code, fuzzer, path, contract_path, method, url, scenario,
#         result_reason, result_details, any_text (reason + details), response_body,
#         response_content_type, request_body, json_body.<dotted.path>,
#         request_header.<name>
# Operators: equals (bare scalar), in, not_equals, contains, contains_any,
#            contains_all, starts_with, starts_with_any, ends_with, matches, exists
# equals/in/not_equals are case-sensitive; the rest are case-insensitive.
version: 1
rules:
  - id: RATE_LIMIT_429
    why: Rate limiting is infrastructure protection, not API behavior under test.
    when: {response_code: 429}

  - id: CONNECTION_ERROR_999
    why: CATS reports 999 for transport/connection failures, not API defects.
    when: {response_code: 999}
"""


def render_init_config(*, spec: str, server: str, health_url: str,
                       results_dir: str, rules: str) -> str:
    return f"""# CATS tooling configuration (machine-local; not committed).
version: 1

spec: {spec}
server: {server}
health_url: {health_url}
results_dir: {results_dir}
false_positives: {rules}

# Delete the raw CATS report after a successful parse (it is large and redundant
# once results are in SQLite). Set to true to keep it.
retain_raw_report: false
# Refuse any rule that would suppress a 5xx response.
allow_suppressing_5xx: false

identities:
  default:
    # Command printing a bearer token on stdout. Nothing else may go to stdout.
    token_cmd: "echo REPLACE_ME"
default_identity: default
auth:
  header: Authorization
  template: "Bearer {{token}}"

# Shell commands run at pipeline stages. Any language or toolchain.
# Each receives CATS_SERVER, CATS_SPEC, CATS_RESULTS_DIR, CATS_REPORT_DIR,
# CATS_RUN_ID and CATS_IDENTITY; post_run also gets CATS_DB and CATS_EXIT_CODE.
hooks:
  seed: ""
  pre_run: ""
  post_run: ""

cats:
  http_methods: [POST, PUT, GET, DELETE, PATCH]
  max_requests_per_minute: 3000
  # ref_data: test/results/cats/cats-test-data.yml
  skip_field_format: []
  skip_field: []
  skip_fuzzers: []
  skip_fuzzers_for_extension: []
  extra_args: ["--printExecutionStatistics"]
"""
```

- [ ] **Step 4: Implement `cats_tool.py`**

```python
#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""CATS fuzzing toolkit: run, parse, classify, query and report API fuzz results."""

from __future__ import annotations

import argparse
import json
import sqlite3
import sys
import webbrowser
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from catslib import report as reporting
from catslib.classify import classify_db
from catslib.config import (INITIAL_RULES_YAML, Config, ConfigError, find_config,
                            load_config, render_init_config)
from catslib.parse import parse_report
from catslib.rules import RuleError, load_rules
from catslib.runner import HookError, PreflightError, execute, preflight, run_id_for
```

Command implementations:

- **`load()`** — helper: `find_config(Path.cwd())`; if `None`, print `"No .local/cats/config.yaml found. Run /cats:init to create one."` and `sys.exit(2)`; else `load_config(path)`.
- **`resolve_db(config, value)`** — helper: `latest`/`None` → `config.results_dir / "latest.db"` resolved through the symlink; error to stderr and exit 2 if missing, naming `results_dir`.
- **`cmd_init`** — if a config already exists, print its path and exit 0 unless `--force`. Otherwise resolve the spec: use `--spec` if given, else glob in order `api-schema/*.json`, `openapi.json`, `openapi.yaml`, `api/openapi*.{json,yaml}`, `docs/openapi*.{json,yaml}`; if exactly one match, use it; if several, print them and require `--spec`; if none and `--non-interactive`, exit 2. Defaults: `--server http://localhost:8080`, `--health-url` = server, `--results-dir test/results/cats`, `--rules test/cats/false-positives.yaml`. Write `.local/cats/config.yaml` from `render_init_config`, create the rules file from `INITIAL_RULES_YAML` if absent, `mkdir` the results dir, and print (a) the two paths written, (b) the `.gitignore` line the results dir needs, (c) a reminder that `token_cmd` must be replaced.
- **`cmd_doctor`** — load config, run `preflight`, print a ✓/✗ line per check (spec, cats binary + version, server health, rules parse + rule count), exit 1 if any failed.
- **`cmd_run`** — `execute(...)` with the parsed flags; print the summary (run id, db path, counts by result from the DB, FP total, top 10 true-positive paths); exit with the CATS exit code. On `HookError`/`PreflightError`/`ConfigError`/`RuleError`, print the message to stderr and exit 2.
- **`cmd_parse`** — `parse_report(report, db or results_dir/cats-results-<run_id_for(now)>.db, {"run_id": ...})`, print stats.
- **`cmd_classify`** — load rules, `classify_db`. With `--dry-run`, copy the DB to a temp file first and report against the copy, leaving the original untouched. Print flagged total, per-rule counts, zero-match rules, delta lists (capped at 20 test ids with an "and N more" line), and any 5xx violations. Exit 1 if violations exist.
- **`cmd_query`** — with `--sql`, execute it and print rows (`--json` for JSON output); without, print the four canned summaries from the legacy `query-cats-results.py` (summary by result excluding FPs, FP count, top-10 error paths, top-10 warn paths) using the same column-aligned `print_table` helper.
- **`cmd_report`** — `reporting.render_html(db)` → `--out` (default `results_dir/report-<run_id>.html`); `--open` calls `webbrowser.open(path.as_uri())`.

- [ ] **Step 5: Run the config tests to verify they pass**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_config.py -v`
Expected: PASS (13 tests)

- [ ] **Step 6: Smoke-test the CLI end to end on a throwaway repo**

```bash
cd $(mktemp -d) && git init -q . && printf '{}' > openapi.json
uv run ~/Projects/skills/cats/scripts/cats_tool.py init --non-interactive
cat .local/cats/config.yaml | head -20
uv run ~/Projects/skills/cats/scripts/cats_tool.py doctor || true
```

Expected: init writes `.local/cats/config.yaml` and `test/cats/false-positives.yaml`, prints the gitignore hint; `doctor` reports ✓ for spec and rules, and ✗ for server health (nothing is running) with a clear message.

- [ ] **Step 7: Commit**

```bash
cd ~/Projects/skills
git add cats/scripts/cats_tool.py cats/scripts/catslib/config.py tests/test_cats_config.py
git commit -m "feat(cats): CLI entrypoint with init and doctor"
```

---

## Task 7: HTML report

**Files:**
- Create: `~/Projects/skills/cats/scripts/catslib/report.py`
- Test: `~/Projects/skills/tests/test_cats_report.py`

**Interfaces:**
- Consumes: a classified DB from Tasks 3–4.
- Produces: `render_html(db_path: Path) -> str`, `summary(db_path: Path) -> dict` (used by both the HTML and `cmd_run`'s printed summary; keys: `run_meta`, `by_result`, `false_positive_total`, `by_rule`, `zero_match_rules`, `true_positives_by_path`, `true_positives`).

- [ ] **Step 1: Write the failing tests**

`tests/test_cats_report.py`:

```python
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cats" / "scripts"))

from catslib import classify as C
from catslib import parse as P
from catslib import report as Rep
from catslib import rules as R

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_cats_classify import build_db, cats_json, write_rules  # noqa: E402


class TestSummary(unittest.TestCase):
    def setUp(self):
        self.db = build_db([cats_json(), cats_json(testId="Test 2", fuzzer="Other")])
        C.classify_db(self.db, write_rules(
            "version: 1\nrules:\n  - id: ONLY_HAPPY\n    why: w\n    when: {fuzzer: HappyPath}\n"),
            allow_5xx=False)

    def test_counts_split_by_classification(self):
        s = Rep.summary(self.db)
        self.assertEqual(s["false_positive_total"], 1)
        self.assertEqual(s["by_rule"]["ONLY_HAPPY"], 1)
        self.assertEqual(len(s["true_positives"]), 1)
        self.assertEqual(s["true_positives"][0]["fuzzer"], "Other")

    def test_zero_match_rules_listed(self):
        C.classify_db(self.db, write_rules(
            "version: 1\nrules:\n  - id: NEVER\n    why: w\n    when: {response_code: 418}\n"),
            allow_5xx=False)
        self.assertEqual(Rep.summary(self.db)["zero_match_rules"], ["NEVER"])


class TestRenderHtml(unittest.TestCase):
    def test_self_contained_and_escaped(self):
        db = build_db([cats_json(scenario="<script>alert(1)</script>")])
        C.classify_db(db, [], allow_5xx=False)
        html = Rep.render_html(db)
        self.assertIn("<!doctype html>", html.lower())
        self.assertNotIn("<script>alert(1)</script>", html)
        self.assertIn("&lt;script&gt;", html)
        for marker in ("http://cdn", "https://cdn", "src=\"http"):
            self.assertNotIn(marker, html)

    def test_reports_true_positive_paths(self):
        db = build_db([cats_json()])
        C.classify_db(db, [], allow_5xx=False)
        self.assertIn("/things", Rep.render_html(db))


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run to verify failure**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_report.py -v`
Expected: FAIL — no module `catslib.report`

- [ ] **Step 3: Implement `report.py`**

`summary(db_path)` runs these queries and returns them as a dict:

```sql
-- run_meta:            SELECT * FROM run_meta LIMIT 1
-- by_result:           SELECT rt.name, COUNT(*) FROM tests t
--                        JOIN result_types rt ON t.result_type_id = rt.id GROUP BY rt.name
-- false_positive_total:SELECT COUNT(*) FROM tests WHERE is_false_positive = 1
-- by_rule:             SELECT rule_id, match_count FROM fp_rules
--                        WHERE match_count > 0 ORDER BY match_count DESC
-- zero_match_rules:    SELECT rule_id FROM fp_rules WHERE match_count = 0 ORDER BY order_index
-- true_positives_by_path:
--   SELECT path, COUNT(*) c, GROUP_CONCAT(DISTINCT fuzzer)
--     FROM true_positives_view GROUP BY path ORDER BY c DESC LIMIT 25
-- true_positives:
--   SELECT test_id, result, fuzzer, path, http_method, response_code, scenario,
--          result_reason FROM true_positives_view ORDER BY response_code DESC, path LIMIT 500
```

`render_html(db_path)` builds one self-contained document: `<!doctype html>`, a `<style>`
block (no external URLs, no `<script src>`), then sections — run provenance, result mix,
false positives by rule, zero-match rules, true positives grouped by path, and a table of
individual true positives. Every interpolated value passes through `html.escape`. Tables
that can be wide sit inside `<div style="overflow-x:auto">`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest -m pytest tests/test_cats_report.py -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Run the whole plugin suite**

Run: `cd ~/Projects/skills && uv run --with pyyaml --with pytest tests/ -k cats -v`
Expected: PASS (all cats tests, ~70)

- [ ] **Step 6: Commit**

```bash
cd ~/Projects/skills
git add cats/scripts/catslib/report.py tests/test_cats_report.py
git commit -m "feat(cats): self-contained HTML report"
```

---

## Task 8: Skills, subagent, and marketplace registration

**Files:**
- Create: `~/Projects/skills/cats/skills/{init,run,report,analyze,fp}/SKILL.md`
- Create: `~/Projects/skills/cats/agents/cats-run.md`
- Modify: `~/Projects/skills/.claude-plugin/marketplace.json`
- Modify: `~/Projects/skills/README.md`

**Interfaces:**
- Consumes: the `cats_tool.py` CLI contract from Task 6.
- Produces: user-facing skills `/cats:init`, `/cats:run`, `/cats:report`, `/cats:analyze`, `/cats:fp`.

Every skill references the tool as `${CLAUDE_PLUGIN_ROOT}/scripts/cats_tool.py`, invoked with `uv run`.

- [ ] **Step 1: Write `skills/init/SKILL.md`**

Frontmatter: `name: init`, `version: 0.1.0`, description covering "bootstrap CATS fuzzing configuration for this repo; use when setting up CATS/API fuzzing for the first time, or when `.local/cats/config.yaml` is missing".

Body: run `cats_tool.py init`; then walk the user through the three things init cannot infer — the `token_cmd` (must print a bearer token on stdout and nothing else), the `seed` hook, and the `pre_run` hook. Include the "offer to add the results dir to `.gitignore`" step explicitly (read `.gitignore`, append only if absent). Finish with `cats_tool.py doctor` and show the output.

- [ ] **Step 2: Write `agents/cats-run.md` and `skills/run/SKILL.md`**

`agents/cats-run.md` frontmatter: `name: cats-run`, `description: Executes a CATS fuzzing campaign end to end and returns a compact summary`, `tools: Bash, Read`. Body: run exactly `uv run ${CLAUDE_PLUGIN_ROOT}/scripts/cats_tool.py run <flags>`; do not interpret findings; do not modify files; return only the run id, DB path, counts by result, FP total, and the top true-positive paths.

`skills/run/SKILL.md`: dispatch that agent in the background; state the expected duration; on `exit 2` report the tool's message verbatim and stop (do not improvise around a failed hook); point at `/cats:init` when unconfigured; point at `/cats:analyze` when the run completes.

- [ ] **Step 3: Write `skills/report/SKILL.md` — the schema reference**

This file is the durable documentation of the database. It must contain:

1. The full table list with every column and type: `tests`, `requests`, `responses`, `request_headers`, `response_headers`, `result_types`, `fuzzers`, `servers`, `paths`, `http_methods`, `run_meta`, `fp_rules`.
2. The views and what each is for: `test_results_view`, `test_results_filtered_view`, `true_positives_view`, `fp_rule_stats_view`, `fuzzer_stats_view`, `path_error_analysis_view`, `response_code_stats_view`.
3. The join shape in prose: `tests` is the hub; `requests`/`responses` are 1:1 on `tests.id`; headers hang off `requests.id`/`responses.id`; the five lookup tables resolve ids to names; **use the views unless you need headers or bodies**.
4. At least six worked queries, each with its purpose: true positives by path; errors by fuzzer; 5xx anywhere (should be empty); FP rules by match count; zero-match rules; response-code distribution for one path.
5. How to generate the HTML report, and that `--db latest` is the default.

Frontmatter description must advertise the schema knowledge, e.g.: "Query and render CATS fuzzing results. Documents the CATS results SQLite schema (tables, views, worked queries) so results can be queried directly. Use when asked about CATS results, fuzzing findings, or to generate a CATS report."

- [ ] **Step 4: Write `skills/analyze/SKILL.md`**

Workflow: resolve the DB (`latest`), pull `true_positives_view`, cluster by (path, fuzzer, response_code), and for each cluster decide one of three dispositions — real bug / spec gap (an undocumented response code for that operation) / false-positive candidate — checking the cluster against the OpenAPI spec named in the config. Output a remediation plan ordered by severity, with **every 5xx first and never dispositioned as a false positive**. FP candidates are handed to `/cats:fp` with a drafted rule. State explicitly that a finding is not dismissed without evidence from the spec or the response.

- [ ] **Step 5: Write `skills/fp/SKILL.md`**

Three modes as specified: `add` (draft the rule, then dry-run via `cats_tool.py classify --dry-run` and show precisely which currently-true-positive tests it would suppress; refuse if any is 5xx; warn if it suppresses more than 5% of remaining true positives; only then write the rule and reclassify), `review` (zero-match and over-broad rules from `fp_rules`), `reclassify` (apply and show the delta). Include the field/operator vocabulary table so rules can be authored without reading the source.

- [ ] **Step 6: Register the plugin**

Add to `.claude-plugin/marketplace.json` `plugins` array:

```json
{ "name": "cats", "description": "Portable CATS API fuzzing toolkit: bootstrap per-repo config (init), run fuzz campaigns through config-declared shell hooks (run), query and render results from SQLite (report), triage true positives into a remediation plan (analyze), and manage declarative false-positive rules (fp). Repo-specific setup lives in .local/cats/config.yaml; false-positive rules live in a committed YAML file.", "source": "./cats", "category": "testing" }
```

Add a `## cats` section to `README.md` mirroring the other plugin entries.

- [ ] **Step 7: Verify no TMI-specific content leaked**

```bash
cd ~/Projects/skills && rg -in "tmi|charlie|threat.model|8079|oauth.stub|api-schema" cats/ && echo "LEAK FOUND" || echo "clean"
```

Expected: `clean`. Any hit must be removed or genericized before committing.

- [ ] **Step 8: Commit**

```bash
cd ~/Projects/skills
git add cats/skills cats/agents .claude-plugin/marketplace.json README.md
git commit -m "feat(cats): skills, run subagent, and marketplace registration"
```

---

## Task 9: Port the 62 TMI rules to YAML

**Files:**
- Create: `/Users/efitz/Projects/tmi/test/cats/false-positives.yaml`
- Reference (read-only): `/Users/efitz/Projects/tmi/scripts/parse_cats_results.py:542-1450`

**Interfaces:**
- Consumes: the rule schema from Task 2.
- Produces: the committed TMI rule set. Task 10 validates it.

- [ ] **Step 1: Port every rule in source order**

Walk `detect_false_positive()` top to bottom. For each numbered block, emit one YAML rule preserving position. Carry the existing explanatory comment into `why` (trimmed to a sentence or two). The `FP_RULE_*` constant's *value* (e.g. `"RATE_LIMIT_429"`) is the `id`.

Translation table for the constructs that appear:

| Legacy Python | YAML |
| --- | --- |
| `response_code == 400` | `response_code: 400` |
| `response_code in [401, 403]` | `response_code: {in: [401, 403]}` |
| `fuzzer == 'ExamplesFields'` | `fuzzer: ExamplesFields` |
| `fuzzer in [list]` | `fuzzer: {in: [...]}` |
| `any(f in fuzzer for f in [a, b])` | `fuzzer: {contains_any: [a, b]}` |
| `'HttpMethods' in fuzzer` | `fuzzer: {contains: HttpMethods}` |
| `path == '/saml/acs'` | `path: {equals: /saml/acs}` |
| `'/oauth2/revoke' in path` | `path: {contains: /oauth2/revoke}` |
| `path.startswith('/admin/')` | `path: {starts_with: /admin/}` |
| `'/a/' in path and '/b' in path` | `path: {contains_all: ["/a/", "/b"]}` |
| `any(msg in result_details for msg in [...])` | `result_details: {contains_any: [...]}` |
| `keyword in f"{reason} {details}"` | `any_text: {contains_any: [...]}` |
| `json_body.get('error_description')` contains | `json_body.error_description: {contains_any: [...]}` |
| `request.httpMethod == 'POST'` | `method: POST` |
| `url.endswith('/')` | `url: {ends_with: "/"}` |
| `response_content_type.startswith('text/plain')` | `response_content_type: {starts_with: text/plain}` |
| request header lookup | `request_header.<name>: {contains: ...}` |

**Two rules that need care:**

- Any sub-condition reading `response.responseBody` is **dead** (the key never exists). Omit that sub-condition and add a comment on the rule: `# NOTE: legacy also checked response.responseBody, which CATS never emits (dead branch).` Where the legacy rule had *only* a `responseBody` check on some path, that path contributes nothing, so the ported rule keeps only the live conditions.
- Where the legacy code short-circuits with an `if fuzzer in [...]: return` followed by a *second* independent check under the same response code (e.g. `NOT_FOUND_404` checks a fuzzer list, then `result_reason`, then legitimate-message lists), emit these as `any_of` blocks in the same rule so the id and position are preserved.

- [ ] **Step 2: Validate the file loads**

```bash
cd ~/Projects/skills && uv run --with pyyaml python -c "
import sys; sys.path.insert(0, 'cats/scripts')
from pathlib import Path
from catslib.rules import load_rules
rs = load_rules(Path('/Users/efitz/Projects/tmi/test/cats/false-positives.yaml'))
print(len(rs), 'rules'); print([r.id for r in rs])"
```

Expected: 62 rules, ids matching the `FP_RULE_*` values, no `RuleError`.

- [ ] **Step 3: Commit (TMI)**

```bash
cd /Users/efitz/Projects/tmi
git add test/cats/false-positives.yaml
git commit -m "feat(cats): port false-positive rules to declarative YAML

Ports the 62 rules from detect_false_positive() into a declarative rule file.
Sub-conditions reading response.responseBody are omitted as dead: CATS never
emits that key.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Df1L1zz3NDsRJvaXwo1oBn"
```

---

## Task 10: Corpus validation — prove classification is unchanged

**Files:**
- Create (throwaway, deleted in this task): `/tmp/cats-migration-diff.py`
- Create: `/Users/efitz/Projects/tmi/docs/superpowers/plans/2026-07-26-cats-rule-migration-report.md`

**Interfaces:**
- Consumes: legacy `scripts/parse_cats_results.py`, new `catslib.rules` + `catslib.parse`, TMI's ported YAML.
- Produces: a signed-off equivalence result. **This is the gate for Task 13.**

- [ ] **Step 1: Write the diff harness**

`/tmp/cats-migration-diff.py`:

```python
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Compare legacy detect_false_positive() with the declarative engine over the corpus."""

import json
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, "/Users/efitz/Projects/tmi/scripts")
sys.path.insert(0, "/Users/efitz/Projects/skills/cats/scripts")

from parse_cats_results import CATSResultsParser
from catslib.parse import record_from_json
from catslib.rules import classify_record, load_rules

REPORT = Path("/Users/efitz/Projects/tmi/test/outputs/cats/report")
RULES = Path("/Users/efitz/Projects/tmi/test/cats/false-positives.yaml")

legacy = CATSResultsParser(":memory:")
rules = load_rules(RULES)

diffs = []
agree = Counter()
legacy_only = Counter()
new_only = Counter()
rule_mismatch = Counter()
total = 0

for path in sorted(REPORT.glob("Test*.json")):
    try:
        data = json.loads(path.read_text())
    except json.JSONDecodeError:
        continue
    total += 1
    old_fp, old_rule = legacy.detect_false_positive(data)
    new_fp, new_rule, _ = classify_record(rules, record_from_json(data), allow_5xx=True)
    if (old_fp, old_rule) == (new_fp, new_rule):
        agree[old_rule or "-"] += 1
        continue
    if old_fp and not new_fp:
        legacy_only[old_rule] += 1
    elif new_fp and not old_fp:
        new_only[new_rule] += 1
    else:
        rule_mismatch[f"{old_rule} -> {new_rule}"] += 1
    if len(diffs) < 50:
        diffs.append((path.name, old_fp, old_rule, new_fp, new_rule))

print(f"total={total} agreements={sum(agree.values())} "
      f"disagreements={total - sum(agree.values())}")
print("legacy-only:", legacy_only.most_common())
print("new-only:", new_only.most_common())
print("rule mismatch:", rule_mismatch.most_common())
for d in diffs:
    print(d)
```

Note `allow_5xx=True` here: the harness measures rule equivalence, and the 5xx guard is a
deliberate new behavior that must not be confused with a porting error. Step 4 measures the
guard separately.

- [ ] **Step 2: Run it**

Run: `uv run /tmp/cats-migration-diff.py 2>&1 | tail -60`
Expected: `total=121940`, `disagreements=0`.

- [ ] **Step 3: Reconcile any disagreements**

For each disagreement, read the legacy block and the ported rule, and fix the YAML. Repeat Step 2 until zero. **Do not** adjust the harness to make a diff disappear. If a diff is genuinely a legacy bug that cannot be reproduced faithfully, record it in the report (Step 5) with the reasoning and get explicit sign-off before proceeding.

- [ ] **Step 4: Measure the new 5xx guard separately**

```bash
uv run python - <<'PY'
import json, sys
from pathlib import Path
sys.path.insert(0, "/Users/efitz/Projects/skills/cats/scripts")
from catslib.parse import record_from_json
from catslib.rules import classify_record, load_rules
rules = load_rules(Path("/Users/efitz/Projects/tmi/test/cats/false-positives.yaml"))
hits = []
for p in Path("/Users/efitz/Projects/tmi/test/outputs/cats/report").glob("Test*.json"):
    try: data = json.loads(p.read_text())
    except json.JSONDecodeError: continue
    _, _, violation = classify_record(rules, record_from_json(data), allow_5xx=False)
    if violation: hits.append((p.name, violation))
print(len(hits), "5xx suppressions refused")
for h in hits[:20]: print(h)
PY
```

Any hits are 500s that the legacy rules were silently swallowing — a genuine finding. Record them in the report; they are candidates for GitHub issues under the zero-500 policy, not for a rule widening.

- [ ] **Step 5: Write the migration report**

`docs/superpowers/plans/2026-07-26-cats-rule-migration-report.md` records: corpus size, agreement count, every reconciled disagreement and its cause, the list of dead `responseBody` sub-conditions dropped (rule id + what it checked), the 5xx-guard hits from Step 4, and a recommendation for each on whether to restore behavior deliberately.

- [ ] **Step 6: Delete the harness and commit**

```bash
rm /tmp/cats-migration-diff.py
cd /Users/efitz/Projects/tmi
git add docs/superpowers/plans/2026-07-26-cats-rule-migration-report.md test/cats/false-positives.yaml
git commit -m "test(cats): verify ported rules match legacy classification on 121,940-test corpus

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Df1L1zz3NDsRJvaXwo1oBn"
```

---

## Task 11: Extract TMI's hook scripts

**Files:**
- Create: `/Users/efitz/Projects/tmi/scripts/cats-token.py`
- Create: `/Users/efitz/Projects/tmi/scripts/cats-prep.py`
- Reference (read-only): `scripts/run-cats-fuzz.py:188-255` (OAuth flow), `:279-305` (Redis)

**Interfaces:**
- Consumes: `scripts/lib/tmi_common.py` (`ensure_oauth_stub`, `log_*`, `get_project_root`).
- Produces: two hook commands. `cats-token.py` prints **only** the access token on stdout — every log line goes to stderr, or the token is unusable.

- [ ] **Step 1: Write `scripts/cats-token.py`**

Move `authenticate_user()` from `run-cats-fuzz.py` verbatim (the `/flows/start` POST, the 10-attempt poll on `/flows/{id}`, the `tokens.access_token` extraction), plus the `ensure_oauth_stub()` call. Arguments: `--user` (default `charlie`), `--server` (default `http://localhost:8080`), `--idp` (default `tmi`). Route every `log_info`/`log_error` to **stderr** and `print(token)` to stdout. Use the uv script header with `requires-python = ">=3.11"`.

- [ ] **Step 2: Verify it prints only a token**

```bash
cd /Users/efitz/Projects/tmi && make dev-up
uv run scripts/cats-token.py --user charlie 2>/dev/null | head -c 40; echo
uv run scripts/cats-token.py --user charlie 2>/dev/null | wc -l
```

Expected: a JWT prefix (`eyJ...`), exactly 1 line on stdout.

- [ ] **Step 3: Write `scripts/cats-prep.py`**

Move `prepare_test_environment()`'s Redis portion and `disable_rate_limits()` (the `_redis_del_pattern` helper and its three patterns: `ip:ratelimit:*:127.0.0.1`, `ip:ratelimit:*:::1`, `auth:ratelimit:*:<user>*`, plus the global `*ratelimit*` clear). Argument: `--user` (default `charlie`). Do **not** move the report-directory cleanup — the plugin owns run directories now.

Note: the legacy helper shells out to `docker exec tmi-redis`. Since `make dev-up` now runs Redis in-cluster, verify which works in the current environment and use that form; if `docker exec tmi-redis` fails, use the equivalent `kubectl exec` against the Redis pod in namespace `tmi-platform`. Confirm by running the script and checking it exits 0.

- [ ] **Step 4: Verify the prep hook runs clean**

Run: `cd /Users/efitz/Projects/tmi && uv run scripts/cats-prep.py --user charlie; echo "exit=$?"`
Expected: `exit=0`, with a log line reporting keys cleared (or none present).

- [ ] **Step 5: Lint and commit**

```bash
cd /Users/efitz/Projects/tmi && make lint
git add scripts/cats-token.py scripts/cats-prep.py
git commit -m "feat(cats): extract token and prep hook scripts

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Df1L1zz3NDsRJvaXwo1oBn"
```

---

## Task 12: TMI configuration and Makefile wrappers

**Files:**
- Create: `/Users/efitz/Projects/tmi/.local/cats/config.yaml` (gitignored)
- Modify: `/Users/efitz/Projects/tmi/.gitignore`
- Modify: `/Users/efitz/Projects/tmi/Makefile:435-462`

**Interfaces:**
- Consumes: Tasks 6, 9, 11.
- Produces: working `make cats-fuzz`, `make cats-seed`, `make query-cats-results`, `make analyze-cats-results`, `make cats-report`.

- [ ] **Step 1: Write the config**

```yaml
version: 1
spec: api-schema/tmi-openapi.json
server: http://localhost:8080
health_url: http://localhost:8080/
results_dir: test/results/cats
false_positives: test/cats/false-positives.yaml
retain_raw_report: false
allow_suppressing_5xx: false

identities:
  admin: {token_cmd: "uv run scripts/cats-token.py --user charlie"}
default_identity: admin
auth: {header: Authorization, template: "Bearer {token}"}

hooks:
  seed: "uv run scripts/run-dbtool.py --config=config-development.yml --user=charlie --provider=tmi --server=http://localhost:8080"
  pre_run: "uv run scripts/cats-prep.py --user charlie"
  post_run: ""

cats:
  http_methods: [POST, PUT, GET, DELETE, PATCH]
  max_requests_per_minute: 3000
  ref_data: test/results/cats/cats-test-data.yml
  skip_field_format: [uuid]
  skip_field: [offset]
  skip_fuzzers: [DuplicateHeaders, LargeNumberOfRandomAlphanumericHeaders, EnumCaseVariantFields, BidirectionalOverrideFields, ResponseHeadersMatchContractHeaders, PrefixNumbersWithZeroFields, ZalgoTextInFields, HangulFillerFields, AbugidasInStringFields, FullwidthBracketsFields, ZeroWidthCharsInValuesFields]
  skip_fuzzers_for_extension:
    - {extension: x-public-endpoint, value: "true", fuzzers: [BypassAuthentication]}
    - {extension: x-cacheable-endpoint, value: "true", fuzzers: [CheckSecurityHeaders]}
    - {extension: x-skip-deleted-resource-check, value: "true", fuzzers: [CheckDeletedResourcesNotAvailable]}
    - {extension: x-skip-idor-check, value: "true", fuzzers: [InsecureDirectObjectReferences]}
  extra_args: ["--printExecutionStatistics"]
```

Cross-check every value against `scripts/run-cats-fuzz.py:312-394` before moving on — the 11 skipped fuzzers and 4 extension rules must match exactly.

**`ref_data` note:** the seed step writes `cats-test-data.yml` into the old
`test/outputs/cats/` location. Check `scripts/run-dbtool.py` for where that path is set; if
it is not configurable, either add a flag or point `ref_data` at the path dbtool actually
writes. Verify the file exists at the configured path after running the seed hook.

- [ ] **Step 2: Update `.gitignore`**

Confirm `.local/` is ignored (`git check-ignore -v .local/cats/config.yaml`); add it if not. Add `test/results/` alongside the existing `test/outputs/` entry.

- [ ] **Step 3: Replace the Makefile targets**

```makefile
CATS := uv run $(HOME)/.claude/plugins/cache/efitz-skills/cats/scripts/cats_tool.py
CATS_USER ?= charlie
CATS_CONFIG ?= config-development.yml
CATS_PROVIDER ?= tmi
CATS_SERVER ?= http://localhost:8080

cats-seed:  ## Seed database for CATS fuzzing
	@uv run scripts/run-dbtool.py --config=$(CATS_CONFIG) --user=$(CATS_USER) --provider=$(CATS_PROVIDER) --server=$(CATS_SERVER)

cats-fuzz:  ## Run CATS API fuzzing (seeds, fuzzes, parses, classifies)
	@$(CATS) run $(if $(ENDPOINT),--path $(ENDPOINT),) $(if $(filter true,$(BLACKBOX)),--blackbox,)

query-cats-results:  ## Query parsed CATS results
	@$(CATS) query

analyze-cats-results: query-cats-results  ## Analyze CATS results

cats-report:  ## Generate an HTML report from the latest run
	@$(CATS) report --open
```

Resolve the actual installed plugin path first (`ls ~/.claude/plugins/cache/`) and use it; if the marketplace is not installed locally, point `CATS` at `~/Projects/skills/cats/scripts/cats_tool.py` and note it in the target's comment.

**`cats-fuzz-oci`:** the legacy target passed `--oci` to seeding. Reproduce it as a second identity/hook variant — add `cats-fuzz-oci` that overrides the seed hook via an env var, or drop the target and document `make cats-fuzz` against an Oracle-backed dev environment. Decide based on what `run-dbtool.py --oci` actually changes; do not silently drop the capability.

- [ ] **Step 4: Verify configuration and a scoped run**

```bash
cd /Users/efitz/Projects/tmi && make dev-up
$(CATS) doctor
make cats-seed
$(CATS) run --path /addons
```

Expected: `doctor` all ✓; the scoped run completes, writes `test/results/cats/cats-results-<ts>.db` and `latest.db`, and prints a summary. Confirm the token never appears: `ps` during the run shows no `Bearer`, and `--path /addons` limits the endpoint set.

- [ ] **Step 5: Commit**

```bash
cd /Users/efitz/Projects/tmi
git add .gitignore Makefile
git commit -m "feat(cats): wire make targets to the portable cats plugin

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Df1L1zz3NDsRJvaXwo1oBn"
```

---

## Task 13: Delete legacy scripts and update all references

**Files:**
- Delete: `scripts/run-cats-fuzz.py`, `scripts/parse_cats_results.py`, `scripts/query-cats-results.py`
- Modify: `CLAUDE.md`, `.claude/skills/test/SKILL.md`, `.claude/skills/test-oci/SKILL.md`, `scripts/test-framework.mk:127`, `scripts/help.py`, `scripts/README.md`, `README.md`, `test/TESTING_STRATEGY.md`

**Precondition:** Task 10 reported zero disagreements and Task 12's scoped run succeeded.

- [ ] **Step 1: Confirm the gate**

Re-read the Task 10 report. If `disagreements` is not 0 and the exceptions were not explicitly signed off, stop and report — do not delete.

- [ ] **Step 2: Delete the scripts**

```bash
cd /Users/efitz/Projects/tmi
git rm scripts/run-cats-fuzz.py scripts/parse_cats_results.py scripts/query-cats-results.py
```

- [ ] **Step 3: Find and fix every dangling reference**

```bash
rg -n "parse_cats_results|query-cats-results|run-cats-fuzz|test/outputs/cats" \
  --glob '!logs/**' --glob '!test/outputs/**' --glob '!docs/superpowers/**' .
```

Update each hit. Specifically:
- `scripts/test-framework.mk:127` — `test/outputs/cats/*` → `test/results/cats/*`
- `CLAUDE.md` CATS section — new make targets, `test/results/cats/`, the rules file at `test/cats/false-positives.yaml`, and that false positives are managed with `/cats:fp` rather than by editing Python
- `.claude/skills/test/SKILL.md` and `test-oci/SKILL.md` — replace `make parse-cats-results` and the inline SQL block with `make cats-fuzz` (which now parses and classifies) and a pointer to `/cats:report` for schema and queries
- `scripts/help.py`, `scripts/README.md`, `README.md`, `test/TESTING_STRATEGY.md` — script list and workflow descriptions

Leave `docs/superpowers/**` history alone.

- [ ] **Step 4: Verify nothing dangles**

```bash
rg -n "parse_cats_results|query-cats-results|run-cats-fuzz" \
  --glob '!docs/superpowers/**' --glob '!logs/**' . ; echo "exit=$?"
make lint
```

Expected: `exit=1` (no matches), lint clean.

- [ ] **Step 5: Commit**

```bash
cd /Users/efitz/Projects/tmi
git add -u && git add CLAUDE.md
git commit -m "refactor(cats): remove legacy CATS scripts in favor of the cats plugin

Classification equivalence verified against the 121,940-test corpus.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Df1L1zz3NDsRJvaXwo1oBn"
```

---

## Task 14: Full end-to-end validation

**Files:** none created; this task validates and reports.

- [ ] **Step 1: Full campaign**

```bash
cd /Users/efitz/Projects/tmi
make dev-down && make clean-everything && make build-server && make dev-up
make cats-fuzz    # ~30-40 minutes
```

Expected: completes, writes a timestamped DB, prints a summary.

- [ ] **Step 2: Compare against the June 30 baseline**

```bash
sqlite3 test/results/cats/latest.db <<'SQL'
.mode column
.headers on
SELECT rt.name AS result, COUNT(*) FROM tests t
  JOIN result_types rt ON t.result_type_id = rt.id GROUP BY rt.name;
SELECT COUNT(*) AS false_positives FROM tests WHERE is_false_positive = 1;
SELECT path, COUNT(*) c FROM true_positives_view GROUP BY path ORDER BY c DESC LIMIT 15;
SELECT COUNT(*) AS five_hundreds FROM true_positives_view WHERE response_code >= 500;
SQL
sqlite3 test/outputs/cats/cats-results.db "SELECT COUNT(*), SUM(is_false_positive) FROM tests;"
```

Expected: totals in the same range as the baseline (121,940 tests / 54,813 FPs), with differences attributable to real code changes since June 30. Investigate any large unexplained swing before signing off. `five_hundreds` should be 0; anything else is a zero-500-policy finding to file.

- [ ] **Step 3: Exercise the skills**

Run `/cats:report` (confirm the HTML opens and renders), then `/cats:analyze` (confirm it produces a remediation plan), then `/cats:fp review` (confirm zero-match rules are listed).

- [ ] **Step 4: Retire the old corpus**

Once Steps 1–3 pass, the June 30 corpus and its 1.3 GB DB are no longer needed:

```bash
du -sh test/outputs/cats
rm -rf test/outputs/cats
```

- [ ] **Step 5: File follow-ups**

Use `/github:create-issue` for: (a) GitHub Wiki CATS pages needing updates for the new tooling; (b) each 5xx surfaced by Task 10 Step 4 or Task 14 Step 2; (c) any dead `responseBody` rule branch the migration report recommends deliberately restoring.

- [ ] **Step 6: Push both repos**

```bash
cd ~/Projects/skills && git push -u origin feat/cats-plugin
cd /Users/efitz/Projects/tmi && git pull --rebase && git push -u origin dev/1.6.0/cats-plugin-extraction
git status   # must show up to date with origin
```

---

## Self-review notes

**Spec coverage:** config contract → Task 1; rule engine incl. 5xx guard → Task 2; schema + `run_meta`/`fp_rules`/`true_positives_view`/bodies → Task 3; separate classify pass + deltas + coverage counts → Task 4; hooks/token/headers-file/pipeline/pruning/`latest.db` → Task 5; init/doctor/CLI → Task 6; HTML report → Task 7; five skills + subagent + marketplace → Task 8; rule migration → Task 9; corpus validation gate → Task 10; TMI hook extraction → Task 11; TMI config + make wrappers → Task 12; deletions + doc updates → Task 13; acceptance criteria 1–5 → Tasks 10, 13 Step 4, 14.

**Open item carried into execution:** Task 12 Step 1 flags that `run-dbtool.py` writes `cats-test-data.yml` to the legacy path, and Task 12 Step 3 flags the `cats-fuzz-oci` seeding variant. Both are resolved during that task rather than assumed away here.

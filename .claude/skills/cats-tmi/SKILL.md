---
name: cats-tmi
description: TMI-specific CATS API fuzzing gates and gotchas. Use before any CATS run, seed, analysis, report, or false-positive work in this repo, alongside the cats:* plugin skills.
---

# CATS API fuzzing in TMI

CATS fuzzes the API via the portable `cats@efitz-skills` plugin, configured per repo in `.local/cats/config.yaml` (gitignored). `make cats-fuzz` / `/cats:run`, `make analyze-cats-results` / `/cats:analyze`, `make cats-report` / `/cats:report`, `/cats:fp` for false-positive rules.

- **The make targets and `/cats:*` skills are the same engine.** The Makefile resolves `CATS_TOOL` to the installed plugin, falling back to a `~/Projects/skills/cats` dev checkout (what the skills use via `${CLAUDE_PLUGIN_ROOT}`). Never hardcode either path; `make ... CATS_TOOL=/path/to/cats_tool.py` overrides. Two copies of the run-validity gates disagreeing is exactly the failure the gates exist to catch.
- **Identity:** comes from `identities:` in the config (`token_cmd` prints a bearer token), selected with `--identity <name>` on the plugin or `/cats:run`, or by setting the default identity for `make cats-fuzz`. `CATS_USER`/`CATS_SERVER`/`CATS_PROVIDER` control only `make cats-seed`.
- **Output:** `test/results/cats/` — one SQLite database per run, `latest.db` → most recent completed run. Analyze by querying SQLite; don't read the HTML or JSON.
- **Fuzz the cluster directly** (`http://rp2:30080`, the k3s-rp NodePort), never through `kubectl port-forward`: a port-forwarded campaign loses ~46% of requests to connection errors that the `CONNECTION_ERROR_999` rule absorbs, so it looks clean while most of the API was never reached (#463/#578). The plugin refuses such a run at preflight (`--allow-port-forward` overrides).
- **Run-validity gates** (a failing run exits 3 and never becomes `latest.db`): transport-error rate (`max_connection_error_pct`, default 1%) and non-false-positive 401 rate (`max_unauthenticated_pct`, default 5%). The second catches a campaign that revokes its own token by fuzzing a logout endpoint and then runs unauthenticated while reporting complete (#591). Such endpoints belong in `cats.skip_paths` (TMI skips `/me/logout`) and can be fuzzed alone with `run --path`.
- Configs must send `If-Match: *` (`cats.headers` in the config, a first-class key since #599) so optimistic-locking preconditions pass instead of tripping the `If-Match` schema (#581).
- **Seeding** runs over loopback (`http://localhost:8080` behind a port-forward), not the NodePort, because macOS can block a freshly built unsigned Go binary such as `tmi-dbtool` from opening TCP connections to a LAN host (#595). Both the `tmi-server` and `postgres` port-forwards must be up before `make cats-seed` or the plugin's seed hook.
- **False positives:** public (21) and cacheable (7) endpoints use `x-public-endpoint` / `x-cacheable-endpoint` to skip inapplicable fuzzers. The rest (e.g. OAuth 401/403) are classified by `test/cats/false-positives.yaml` (file order, first match wins; see `test/cats/README.md`) into the results DB's `is_false_positive` column. Manage rules through `/cats:fp`, not by editing Python; the legacy `detect_false_positive()` no longer exists.

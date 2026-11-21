# Settings precedence follow-ups (#1169, #1168, #1167, #1163)

Spec: `docs/superpowers/specs/2026-11-20-adr-settings-precedence-row-wins.md` (human decisions, binding). Also read `docs/superpowers/specs/2026-11-12-adr-settings-put-hot-only.md`, which the new ADR amends.

Branch: `dev/1.21/settings-fixes` (work directly on it; no worktree). Tasks run in order; each is one commit with `Fixes #N` in the body.

## Global Constraints

- Make targets only: `make test-unit` (`name=TestX` for one test), `make lint`, `make build-server`, `make verify` (done gate). Never `go test`/`go run` directly. Verbose unit output lands in `/tmp/tmi-test-unit-*`; use `rg -n '^--- FAIL' <log>`.
- Logging: only `github.com/ericfitz/tmi/internal/slogging`. Log setting keys and outcomes, never setting values.
- Zero 500s; the server must return only status codes the OpenAPI spec documents for the operation. If `api-schema/tmi-openapi.json` changes: `make validate-openapi`, then `make generate-api` (oapi-codegen v2.7.1). Never edit `.version`, `api/version.go`, or the spec `info.version`.
- Empty row value is treated as absent (Oracle binds '' as NULL); keep that collapse.
- TDD: write the failing test that reproduces the bug first, confirm it fails, then fix.
- After a fix, search for every other instance of the same pattern and fix or list it.
- Conventional commit subject `fix(<scope>): ...`; body ends with:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_011fnCFsyWGeAYL1Zs3jmUmf`.
- Use `rg PATTERN <path>` (always give a path). zsh does not word-split unquoted vars.

## Task 1: Unreadable settings row must not cause a 500 (#1169)

Read `gh issue view 1169`. In `api/settings_service.go`, a `system_settings` row that cannot be decrypted (Get ~226-231, List ~447-452) currently returns an error, so `GET /admin/settings` and `GET /admin/settings/{key}` return 500.

Required behavior:
- The precedence resolver (`GetResolvedString` and anything sharing `configOutranksRow`) treats an undecryptable row as unusable (same as no row): fall back to the config layer. Log a Warn with the key and outcome only.
- `List` (and the `GET /admin/settings` handler) does not fail as a whole: an unreadable row is still listed with its value masked and a clear marker (reuse an existing masking convention / field if the response schema has one; if a new field is required, add it to the spec, describe it, and regenerate). Choose the smallest spec-compliant option and justify it in the report.
- `GET /admin/settings/{key}` for an unreadable row: return the config-layer value if config supplies one; otherwise a documented non-500 response (check the spec for the operation's documented codes; reuse the `ReEncryptAll` / `DetailUnreadableSettingsLimit` style detail code if appropriate).
- Unit tests reproducing each 500 first (fake/in-memory decrypt failure).

## Task 2: One precedence rule, admin-set row wins; PUT allowed when config is explicit (#1168)

Read `gh issue view 1168`. `getConfigSetting` (`api/settings_service.go` ~161, used by `GetString`/`GetBool`/`GetInt`) lets explicit config always win. Route it through `configOutranksRow` so all read paths agree with `GetResolvedString`:

| Case | Result |
| --- | --- |
| Explicit (admin-set/imported) row | row |
| Seeded default row + explicit config | config |
| Seeded default row + non-explicit config | row |
| No usable row (missing, empty, or unreadable per Task 1) | config layer (explicit or default) |

- Write the issue's repro as a failing test first (config not set, admin row `timmy.enabled=false`, then explicit `TMI_TIMMY_ENABLED=true` → `GetBool` must return false).
- The "no usable row + non-explicit config" case changes `""` to the config default: audit every `GetString`/`GetBool`/`GetInt` caller that treats `""`/not-found as unset (e.g. `boolSettingIfPresent` in `api/config_handlers.go`, `TimmyConfigProvider`, others found by `rg`), and confirm behavior stays correct. List each in the report.
- `rg` for other read paths that bypass the resolver (direct `GetMigratableSettings()` consumers that decide a value, admin handlers, public client config) and route them through it or justify why not.
- `configWriteBlock` (`api/config_handlers.go` ~987): drop the `entry.Explicit` block **only** for keys whose def is Operational and Hot. Static, bootstrap, provider-subtree and no-def config-known keys still return 409. Check whether DELETE shares the guard and apply the same rule consistently.
- Update spec text for the PUT 409 description if it says explicit config blocks writes; regenerate per Global Constraints.
- Update `TestRuntimeWritableConfigSettings_ExactSet` and any explicit-config 409 tests to the new rule. Fix stale doc comments in `settings_service.go` describing "environment/config file > database".

## Task 3: auth.oauth_callback_url and features.saml_enabled are startup-only (#1167)

Read `gh issue view 1167`. Decision: both come from config/env only.

- Remove the runtime database reads: `RuntimeConfigReaderAdapter.IsSAMLEnabled` / `GetOAuthCallbackURL` (`api/runtime_config_reader_adapter.go`) and their callers in `auth/handlers.go` (~209, ~218) must use the startup config value (`h.config` / struct). Remove the interface methods from `auth/runtime_config_reader.go` if nothing else needs them.
- Public client config (`api/config_handlers.go` ~250) serves `features.saml_enabled` from the startup config value, not a settings-service read.
- Reclassify both keys (`internal/config/classification_registry.go` ~133, ~140 and their `SettingDef`s in `internal/config/setting_defs_auth.go`) as Static and not operational (pick the existing non-operational category that fits startup config; justify). PUT on both must return 409 (add tests).
- Fix the path-vs-key mix-up generically: `OperationalKeysInFile` (`internal/config/config.go` ~1363) and dbtool `pruneOperationalFromMapping` (`cmd/dbtool/config_strip.go` ~85) must classify by setting key (map YAML path → key the way #1166 did for `GetMigratableSettings`; see commit b219e73a). After this, the drift warning and `dbtool config import-legacy` leave these two keys in the file because they are non-operational, and `extraction.async_enabled` (`content_extractors.async_enabled`) still behaves as operational. Tests for all three.
- Update pinned tests (`classification_registry_test.go` ~95, `migratable_settings_equivalence_test.go` ~102, `migratable_settings_test.go` callback tests ~185-245) and `cmd/dbtool/config_export.go` notes as needed. Keep the TMI_OAUTH_CALLBACK_URL env handling working.
- Existing DB rows for these keys become inert; no migration needed.

## Task 4: timmy.* mutability labels match runtime readers (#1163)

Read `gh issue view 1163`. For each `timmy.*` def in `internal/config/setting_defs_content.go`, set Mutability Hot if `TimmyConfigProvider` (`api/timmy_config_provider.go`) reads it from the settings service at runtime, Static otherwise; verify key by key, do not flip blindly. Fix the comment at ~15-23. For newly writable secret keys, confirm (with a test) that PUT stores them encrypted and GET masks them. Update `TestRuntimeWritableConfigSettings_ExactSet` to the final set.

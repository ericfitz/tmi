# ReEncryptAll Audit Fields (#805) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `SettingsService.ReEncryptAll` write only the ciphertext so a key rotation never touches `modified_by` / `modified_at`, and drop its now-meaningless `modifiedBy` parameter.

**Architecture:** `ReEncryptAll` currently loads every `system_settings` row, mutates `Value`, `ModifiedAt`, `ModifiedBy`, and calls a full-struct GORM `Save`. It will instead issue `UpdateColumn("value", ...)` scoped by `setting_key`, which bypasses GORM's `autoUpdateTime` hook and never mentions the audit columns. The actor of a rotation is already recorded by `AdminAuditMiddleware` (descriptor for `POST /admin/settings/reencrypt`, summary `REENCRYPT system_settings`, in `api/admin_audit_descriptors.go:63-70`), so the row no longer needs to claim it. The `#794` origin backfill predicate (`modified_by IS NOT NULL` means operator intent) is unchanged; only its "known gap" comment shrinks.

**Tech Stack:** Go, GORM (`UpdateColumn`), SQLite in-memory for unit tests (`setupSettingsTestDB`), testify, Make targets.

**Spec:** `docs/superpowers/specs/2026-09-04-reencrypt-audit-fields-design.md` (verbatim copy of the approved 2026-09-04 design comment on GitHub issue #805).

## Global Constraints

- **Always use Make targets.** Never run `go test`, `go run`, or `./bin/tmiserver` directly. Single test: `make test-unit name=TestName count1=true passfail=true` (the `name` value must not contain `|`). Lint: `make lint`. Build: `make build-server`. Integration: `make test-integration`.
- **gofmt** everything. Imports grouped stdlib / external / internal.
- **SEM markers:** every function whose behavior changes gets the description text of its `// SEM@<sha>: <intent>` comment updated. Keep the existing sha; tooling refreshes it later. Unchanged functions keep their markers untouched.
- **Logging:** only `github.com/ericfitz/tmi/internal/slogging` (already what these files use).
- **Branch:** all work on `dev/1.9.16/reencrypt-audit-fields` off `main`. `main` is PR-only; never commit to it.
- **Commits:** conventional commits. The resolving commit body contains `Fixes #805`. Every commit ends with this trailer (two lines, verbatim):

  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv
  ```

- **Oracle review is mandatory** (project policy): this change touches a `system_settings` write path, so the `oracle-db-admin` subagent must review the diff before the work is reported complete, and its verdict must be addressed.
- **Not in scope** (spec): no remediation of existing rows (`reencrypt` has never been run on any install), no dedicated intent marker (option 3), no change to the backfill predicate.

---

### File map

| File | Change |
|---|---|
| `api/settings_service.go:665-719` | `ReEncryptAll`: drop `modifiedBy` param; replace `Save` with `UpdateColumn("value", ...)`; update SEM description |
| `api/server.go:36` | `SettingsServiceInterface.ReEncryptAll` signature |
| `api/config_handlers.go:828-836` | `ReencryptSystemSettings`: remove the `userInternalUUID` lookup; call `ReEncryptAll(ctx)` |
| `api/config_handlers_test.go:105-107` | `MockSettingsService.ReEncryptAll` signature |
| `api/runtime_config_reader_adapter_test.go:74-76` | `fakeSettingsService.ReEncryptAll` signature |
| `api/settings_service_test.go:710-759` | Extend `TestSettingsService_ReEncryptAll_PreservesOrigin` with audit-field assertions |
| `internal/dbschema/system_setting_origin_backfill.go:105-116` | Shorten the "known residual gap" comment |

---

### Task 1: ReEncryptAll writes only the ciphertext and drops `modifiedBy`

**Files:**
- Modify: `api/settings_service.go:665-719`
- Modify: `api/server.go:36`
- Modify: `api/config_handlers.go:828-836`
- Modify: `api/config_handlers_test.go:105-107`
- Modify: `api/runtime_config_reader_adapter_test.go:74-76`
- Modify: `internal/dbschema/system_setting_origin_backfill.go:105-116`
- Test: `api/settings_service_test.go:710-759`

**Interfaces:**
- Consumes: `models.SystemSetting` (`api/models/system_setting.go`: fields `SettingKey DBVarchar`, `Value DBText`, `ModifiedAt time.Time` with `autoUpdateTime`, `ModifiedBy NullableDBVarchar`, `Origin NullableDBVarchar`); `models.NewNullableDBVarchar(*string) NullableDBVarchar` (`api/models/types.go:797`); `crypto.NewSettingsEncryptorFromKeys(key, prev []byte, version int)` and its `Encrypt(string) (string, error)` / `Decrypt(string) (string, error)`; `setupSettingsTestDB(t) *gorm.DB` (`api/settings_service_test.go:24`).
- Produces: `func (s *SettingsService) ReEncryptAll(ctx context.Context) (int, []SettingError, error)` and the matching method on `SettingsServiceInterface`, `MockSettingsService`, and `fakeSettingsService`. Task 2 and Task 3 rely on this signature.

- [ ] **Step 1: Create the working branch**

```bash
cd /Users/efitz/Projects/tmi
git checkout main
git pull --rebase
git checkout -b dev/1.9.16/reencrypt-audit-fields
```

Expected: `Switched to a new branch 'dev/1.9.16/reencrypt-audit-fields'`.

- [ ] **Step 2: Rewrite the failing test**

Replace the whole of `TestSettingsService_ReEncryptAll_PreservesOrigin` and its leading comment (`api/settings_service_test.go:710-759`, from the line `// TestSettingsService_ReEncryptAll_PreservesOrigin is the #794 regression` through the closing `}` of the function) with:

```go
// TestSettingsService_ReEncryptAll_PreservesOrigin is the #794/#805
// regression guard: ReEncryptAll is a mechanical key-rotation pass, not an
// operator edit, so it must write only the ciphertext. It must NOT touch
// Origin (if key rotation promoted every seeded row to explicit, every
// operational key would suddenly out-rank config) and it must NOT touch
// modified_by or modified_at (the #794 origin backfill reads modified_by as
// operator intent, and a nil actor used to clear it to NULL).
func TestSettingsService_ReEncryptAll_PreservesOrigin(t *testing.T) {
	gormDB := setupSettingsTestDB(t)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	enc, err := crypto.NewSettingsEncryptorFromKeys(key, nil, 1)
	require.NoError(t, err)

	svc := NewSettingsService(gormDB, nil)
	svc.SetEncryptor(enc)

	// A fixed past timestamp: autoUpdateTime only fills modified_at on Create
	// when it is zero, so this value is what lands in the row.
	stampedAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	operator := "11111111-2222-3333-4444-555555555555"

	seeded := models.SystemSetting{
		SettingKey:  "session.timeout_minutes",
		Value:       "60", // plaintext; ReEncryptAll must accept this as well as already-encrypted values
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  stampedAt,
		// ModifiedBy deliberately NULL: nobody ever set this row.
		Origin: models.NullableDBVarchar{String: models.SystemSettingOriginSeeded, Valid: true},
	}
	require.NoError(t, gormDB.Create(&seeded).Error)

	explicit := models.SystemSetting{
		SettingKey:  "ui.default_theme",
		Value:       "dark",
		SettingType: models.SystemSettingTypeString,
		ModifiedAt:  stampedAt,
		ModifiedBy:  models.NewNullableDBVarchar(&operator),
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
	}
	require.NoError(t, gormDB.Create(&explicit).Error)

	count, settingErrors, err := svc.ReEncryptAll(context.Background())
	require.NoError(t, err)
	assert.Empty(t, settingErrors)
	assert.Equal(t, 2, count)

	var reloadedSeeded models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "session.timeout_minutes").First(&reloadedSeeded).Error)
	assert.Equal(t, models.SystemSettingOriginSeeded, reloadedSeeded.Origin.String,
		"ReEncryptAll must not promote a seeded row to explicit")
	assert.False(t, reloadedSeeded.IsExplicit())
	assert.False(t, reloadedSeeded.ModifiedBy.Valid,
		"ReEncryptAll must leave a NULL modified_by NULL")
	assert.True(t, stampedAt.Equal(reloadedSeeded.ModifiedAt),
		"ReEncryptAll must not move modified_at (got %v)", reloadedSeeded.ModifiedAt)
	assert.NotEqual(t, "60", string(reloadedSeeded.Value), "value must now be ciphertext")
	plain, err := enc.Decrypt(string(reloadedSeeded.Value))
	require.NoError(t, err)
	assert.Equal(t, "60", plain)

	var reloadedExplicit models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "ui.default_theme").First(&reloadedExplicit).Error)
	assert.Equal(t, models.SystemSettingOriginExplicit, reloadedExplicit.Origin.String)
	assert.True(t, reloadedExplicit.IsExplicit())
	require.True(t, reloadedExplicit.ModifiedBy.Valid, "ReEncryptAll must not clear modified_by")
	assert.Equal(t, operator, reloadedExplicit.ModifiedBy.String,
		"ReEncryptAll must not overwrite modified_by")
	assert.True(t, stampedAt.Equal(reloadedExplicit.ModifiedAt),
		"ReEncryptAll must not move modified_at (got %v)", reloadedExplicit.ModifiedAt)
	assert.NotEqual(t, "dark", string(reloadedExplicit.Value), "value must now be ciphertext")
	plain, err = enc.Decrypt(string(reloadedExplicit.Value))
	require.NoError(t, err)
	assert.Equal(t, "dark", plain)
}
```

`time` is already imported in this test file (`api/settings_service_test.go:7`); no import changes.

- [ ] **Step 3: Run the test to verify it fails**

Run:

```bash
make test-unit name=TestSettingsService_ReEncryptAll_PreservesOrigin count1=true passfail=true
```

Expected: FAIL to compile with a message like `not enough arguments in call to svc.ReEncryptAll` (the test uses the new one-argument signature). That compile error is the red step for this task.

- [ ] **Step 4: Change `ReEncryptAll` in `api/settings_service.go`**

Replace lines 665-668 (the doc comment, SEM marker, and signature):

```go
// ReEncryptAll re-encrypts all settings with the current encryption key.
// Returns the count of settings re-encrypted, any per-setting errors, and a fatal error if applicable.
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: re-encrypt all stored settings with the current encryption key (reads DB)
func (s *SettingsService) ReEncryptAll(ctx context.Context, modifiedBy *string) (int, []SettingError, error) {
```

with:

```go
// ReEncryptAll re-encrypts all settings with the current encryption key.
// Returns the count of settings re-encrypted, any per-setting errors, and a fatal error if applicable.
//
// Only the value column is written. Re-encryption is a mechanical key
// rotation, not an operator edit, so modified_at and modified_by are left
// exactly as they were: the #794 origin backfill reads modified_by as
// operator intent, and only SettingsService.Set may create that signal. The
// actor of a rotation is recorded by AdminAuditMiddleware
// (REENCRYPT system_settings), not by the row (#805).
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: re-encrypt every stored setting value in place without touching audit fields (writes DB)
func (s *SettingsService) ReEncryptAll(ctx context.Context) (int, []SettingError, error) {
```

Then replace the "Update in database" block (lines 701-709):

```go
		// Update in database
		setting.Value = models.DBText(encrypted)
		setting.ModifiedAt = time.Now()
		setting.ModifiedBy = models.NewNullableDBVarchar(modifiedBy)
		if err := s.gormDB.WithContext(ctx).Save(&setting).Error; err != nil {
			logger.Error("Failed to save re-encrypted setting %s: %v", setting.SettingKey, err)
			settingErrors = append(settingErrors, SettingError{Key: string(setting.SettingKey), Error: err.Error()})
			continue
		}
```

with:

```go
		// Write only the ciphertext. UpdateColumn skips GORM hooks, so
		// autoUpdateTime does not move modified_at and modified_by is never
		// mentioned in the statement (a full-struct Save would write both).
		if err := s.gormDB.WithContext(ctx).
			Model(&models.SystemSetting{}).
			Where("setting_key = ?", setting.SettingKey).
			UpdateColumn("value", models.DBText(encrypted)).Error; err != nil {
			logger.Error("Failed to save re-encrypted setting %s: %v", setting.SettingKey, err)
			settingErrors = append(settingErrors, SettingError{Key: string(setting.SettingKey), Error: err.Error()})
			continue
		}
```

The `time` import stays: `Set` (line 549), `SeedDefaults` (615), and line 647 still use `time.Now()`.

- [ ] **Step 5: Change the interface in `api/server.go`**

Replace line 36:

```go
	ReEncryptAll(ctx context.Context, modifiedBy *string) (int, []SettingError, error)
```

with:

```go
	ReEncryptAll(ctx context.Context) (int, []SettingError, error)
```

The interface's SEM marker (line 20) describes the whole interface and stays as is.

- [ ] **Step 6: Change the handler in `api/config_handlers.go`**

Delete lines 828-835 (the `userInternalUUID` lookup, including the blank line after it):

```go
	// Get current user UUID for modified_by
	var modifiedBy *string
	if userUUID, exists := c.Get("userInternalUUID"); exists {
		if uuidStr, ok := userUUID.(string); ok {
			modifiedBy = &uuidStr
		}
	}

```

and change line 836 from:

```go
	reencrypted, settingErrors, err := s.settingsService.ReEncryptAll(ctx, modifiedBy)
```

to:

```go
	reencrypted, settingErrors, err := s.settingsService.ReEncryptAll(ctx)
```

Leave the handler's SEM marker (line 802) unchanged: the handler still re-encrypts all settings; only what it forwards changed. (The other `userInternalUUID` lookup at lines 672-676 belongs to the PUT handler and stays.)

- [ ] **Step 7: Update the two mocks**

`api/config_handlers_test.go:105-107`, replace:

```go
func (m *MockSettingsService) ReEncryptAll(ctx context.Context, modifiedBy *string) (int, []SettingError, error) {
	return 0, nil, nil
}
```

with:

```go
func (m *MockSettingsService) ReEncryptAll(ctx context.Context) (int, []SettingError, error) {
	return 0, nil, nil
}
```

`api/runtime_config_reader_adapter_test.go:74-76`, replace:

```go
func (f *fakeSettingsService) ReEncryptAll(ctx context.Context, modifiedBy *string) (int, []SettingError, error) {
	return 0, nil, nil
}
```

with:

```go
func (f *fakeSettingsService) ReEncryptAll(ctx context.Context) (int, []SettingError, error) {
	return 0, nil, nil
}
```

- [ ] **Step 8: Shorten the known-gap comment in the backfill**

In `internal/dbschema/system_setting_origin_backfill.go`, replace lines 105-116:

```go
// Known residual gap (oracle-db-admin review, #794, round 2): the
// modified_by signal is also set by ReEncryptAll on every row it
// re-encrypts (api/settings_service.go), which is a mechanical key-rotation
// pass, not an operator changing a value. A pre-#794 install that ever ran
// key rotation therefore has modified_by on every row and this backfill
// stamps all of them explicit, including genuinely seeded rows -- reviving
// the DB-always-wins outage class through the modified_by door rather than
// the encryption door this function otherwise closes. Not fixed here: it
// reproduces exactly the pre-#794 behavior for that narrow case (no new
// failure mode) and the correct fix is in ReEncryptAll itself (it arguably
// should not touch modified_by/modified_at for a mechanical re-encryption at
// all), not in this backfill's predicate.
```

with:

```go
// The modified_by signal is written only by SettingsService.Set (the admin
// PUT and dbtool import paths). ReEncryptAll used to stamp it on every row
// it rotated, which would have made this backfill read a seeded row as
// operator-set; since #805 it writes only the ciphertext column, so key
// rotation can never manufacture intent. No install ever ran reencrypt
// before that fix, so no remediation of existing rows is needed.
```

The function's SEM marker (line 121) is unchanged: the backfill's behavior did not change.

- [ ] **Step 9: Format and run the test to verify it passes**

Run:

```bash
gofmt -l api internal
make test-unit name=TestSettingsService_ReEncryptAll_PreservesOrigin count1=true passfail=true
```

Expected: `gofmt -l` prints nothing; the test reports PASS.

If `modified_at` assertions fail with a value equal to "now", `UpdateColumn` was not used (a `Save`/`Update`/`Updates` slipped in). If `modified_by` on `ui.default_theme` comes back NULL, the statement is still writing that column.

- [ ] **Step 10: Run the whole `api` and `dbschema` unit suites, then lint**

Run:

```bash
make test-unit count1=true passfail=true
make lint
```

Expected: all unit tests PASS; `make lint` exits 0 with no findings. `unparam` and `revive` are enabled in `.golangci.yml`, which is why the parameter is dropped in the same change rather than left unused.

- [ ] **Step 11: Commit**

```bash
cd /Users/efitz/Projects/tmi
git add api/settings_service.go api/server.go api/config_handlers.go \
  api/config_handlers_test.go api/runtime_config_reader_adapter_test.go \
  api/settings_service_test.go internal/dbschema/system_setting_origin_backfill.go
git commit -F - <<'EOF'
fix(settings): stop ReEncryptAll stamping modified_by and modified_at

Re-encryption is a mechanical key-rotation pass, not an operator edit, but
ReEncryptAll re-Saved every row it touched, moving modified_at via
autoUpdateTime and overwriting modified_by (a nil actor cleared it to NULL).
The #794 origin backfill reads modified_by as operator intent, so a rotated
install would have had every seeded row promoted to explicit.

Write only the value column with UpdateColumn, which fires no hooks and
never mentions the audit columns, and drop the modifiedBy parameter from
ReEncryptAll and SettingsServiceInterface. The actor of a rotation is
already recorded by AdminAuditMiddleware (REENCRYPT system_settings).
Shorten the backfill's known-gap comment accordingly.

Fixes #805

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv
EOF
```

Expected: one commit on `dev/1.9.16/reencrypt-audit-fields`; `git status` shows only the two untracked `docs/superpowers` spec and plan files (the planning session left them uncommitted; the team lead decides where they land).

---

### Task 2: Build and integration gates

**Files:**
- No source changes. Verification only (project task-completion checklist steps 1 and 3).

**Interfaces:**
- Consumes: the Task 1 commit.
- Produces: evidence that the server builds and the admin reencrypt endpoint still behaves (`POST /admin/settings/reencrypt` returns 200 with encryption enabled, 409 otherwise; the audit middleware still records `REENCRYPT system_settings`).

- [ ] **Step 1: Build the server**

Run:

```bash
make build-server
```

Expected: exits 0, `bin/tmiserver` produced.

- [ ] **Step 2: Run the integration suite**

Run:

```bash
make test-integration
```

Expected: PASS. If the run fails because of environment (a foreign listener on `:8080` or `:6379`, or the dev stack is down), do not skip it silently: check `lsof -i :8080 -i :6379`, bring the stack up with `make dev-up` if needed, and re-run. Report any test that still fails with its output; do not disable it.

- [ ] **Step 3: Confirm the audit trail still names the actor (manual, only if the dev stack is up)**

With `make dev-up` running and the OAuth stub started (`make start-oauth-stub`), obtain an admin token for `charlie` and call the endpoint:

```bash
curl -s -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid": "charlie"}'
TOKEN=$(curl -s "http://localhost:8079/creds?userid=charlie" | jq -r '.access_token')
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/admin/settings/reencrypt \
  -H "Authorization: Bearer $TOKEN"
```

Expected: `200` when the settings encryption key is configured in `config-development.yaml`, otherwise `409` (`encryption_not_enabled`). In the `200` case, the `system_audit_entries` table gains a row with summary `REENCRYPT system_settings` and `charlie`'s internal UUID as actor (query it with the `db` skill), while `system_settings.modified_at` and `modified_by` are unchanged for every row. Nothing to commit for this task.

---

### Task 3: Oracle review, then push and open the PR

**Files:**
- Possibly modify `api/settings_service.go` if the review returns findings.

**Interfaces:**
- Consumes: the diff `git diff main...dev/1.9.16/reencrypt-audit-fields`.
- Produces: an `oracle-db-admin` verdict recorded in the final report, and a PR against `main`.

- [ ] **Step 1: Dispatch the `oracle-db-admin` subagent**

Use the Agent tool with `subagent_type: oracle-db-admin` and this prompt (adjust nothing except the branch name if it differs):

```
Review branch dev/1.9.16/reencrypt-audit-fields in /Users/efitz/Projects/tmi
against main (`git diff main...dev/1.9.16/reencrypt-audit-fields`) for Oracle
ADB compatibility. Context: GitHub issue #805, approved design in
docs/superpowers/specs/2026-09-04-reencrypt-audit-fields-design.md.

The only DB-touching change is in api/settings_service.go ReEncryptAll: a
full-struct gorm Save of a system_settings row is replaced by
  gormDB.Model(&models.SystemSetting{}).Where("setting_key = ?", key).UpdateColumn("value", models.DBText(ciphertext))
so that modified_at (autoUpdateTime) and modified_by are not written.
`value` is a DBText (CLOB on Oracle) column; setting_key is the VARCHAR2
primary key. Please check: CLOB bind of DBText through UpdateColumn on
godror, that no hook or default re-introduces modified_at, prepared-statement
cache interaction (this is DML, one statement per row, distinct bind values),
and anything else Oracle-specific. Return one verdict: APPROVED, APPROVED
WITH NOTES, or BLOCKING ISSUES, with each finding tied to a file:line.
```

- [ ] **Step 2: Address the verdict**

- `APPROVED`: note it in the final report and continue.
- `APPROVED WITH NOTES`: apply every easy item now in `api/settings_service.go`, re-run Task 1 Step 9 and Step 10, and commit with:

  ```bash
  git commit -F - <<'EOF'
  fix(settings): address oracle-db-admin notes on ReEncryptAll write path

  <one line per note applied>

  Refs #805

  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv
  EOF
  ```

  File a follow-up issue for anything deferred.
- `BLOCKING ISSUES`: fix every item the same way, re-run the gates, re-dispatch the subagent with the new diff. If a finding seems wrong, do not argue it away; report it to the user for adjudication instead.

- [ ] **Step 3: Push the branch and open the PR**

```bash
cd /Users/efitz/Projects/tmi
git pull --rebase origin main
git push -u origin dev/1.9.16/reencrypt-audit-fields
gh pr create --base main --head dev/1.9.16/reencrypt-audit-fields \
  --title "fix(settings): stop ReEncryptAll stamping modified_by and modified_at" \
  --body-file - <<'EOF'
Re-encryption is a mechanical key-rotation pass, not an operator edit. `ReEncryptAll` now writes only the ciphertext with `UpdateColumn`, so `modified_at` does not move and `modified_by` is never written or cleared. The actor is already recorded by `AdminAuditMiddleware` (`REENCRYPT system_settings`). The `modifiedBy` parameter is dropped from `ReEncryptAll` and `SettingsServiceInterface`.

Spec: `docs/superpowers/specs/2026-09-04-reencrypt-audit-fields-design.md`
Plan: `docs/superpowers/plans/2026-09-04-reencrypt-audit-fields.md`

Oracle review (`oracle-db-admin`): <verdict>

Fixes #805

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01NGXjYqgM7QBAjhNZLFCriv
EOF
git status
```

Expected: `git status` reports the branch is up to date with `origin/dev/1.9.16/reencrypt-audit-fields`; `gh pr create` prints the PR URL. The PR title starts with `fix:` so the version-bump workflow computes a PATCH bump to 1.9.16 on the branch. The issue closes automatically when the PR merges to `main` because the resolving commit body carries `Fixes #805`; if it is merged some other way, close #805 manually with a comment linking the PR.

---

## Self-review

- **Spec coverage:** item 1 (ciphertext-only `UpdateColumn`) is Task 1 Step 4; item 2 (drop the parameter across interface, handler, both mocks) is Task 1 Steps 4-7; item 3 (backfill comment shortened, predicate untouched) is Task 1 Step 8; item 4 (test seeds `modified_by`/`modified_at`, asserts both unchanged, NULL stays NULL, ciphertext changes) is Task 1 Step 2; item 5 (Oracle review) is Task 3. "Not doing" items are listed under Global Constraints.
- **Placeholders:** none. Every code step shows the exact before/after text; the only angle-bracket fields are in the Task 3 commit and PR bodies, filled from the review verdict at execution time.
- **Type consistency:** `ReEncryptAll(ctx context.Context) (int, []SettingError, error)` is used identically in the service, interface, handler, both mocks, and the test. `models.DBText`, `models.NullableDBVarchar`, `models.NewNullableDBVarchar`, `IsExplicit()`, and the encryptor's `Decrypt(string) (string, error)` all match `api/models/` and existing usage in `api/settings_service.go`.

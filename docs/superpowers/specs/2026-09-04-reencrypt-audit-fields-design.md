# Approved design for #805 (verbatim copy of the 2026-09-04 issue comment by ericfitz; human decision)

## Design (approved 2026-09-04, human decision)

**Decision:** option 1, because option 2 already exists. `AdminAuditMiddleware` has a descriptor for `POST /admin/settings/reencrypt` (`api/admin_audit_descriptors.go`, summary `REENCRYPT system_settings`) that records the actor in `system_audit_entries` independently of the row. Re-encryption is a mechanical pass, not an edit, so `ReEncryptAll` stops claiming to be one. No remediation step: `reencrypt` has never been run on any install (AWS, k3s-rp, tmiadb), so no rows were wrongly stamped.

1. **Write only the ciphertext.** Replace the full-struct `Save` in `ReEncryptAll` (`api/settings_service.go` ~700-708) with `UpdateColumn("value", ...)` scoped by `setting_key`. `autoUpdateTime` does not fire for `UpdateColumn`, so `modified_at` stays put and `modified_by` is never written. This also removes a latent bug: a nil `modifiedBy` currently clears an existing `modified_by` to NULL.
2. **Drop the `modifiedBy` parameter.** Signature becomes `ReEncryptAll(ctx) (int, []SettingError, error)`. Update `SettingsServiceInterface` (`api/server.go`), the handler `ReencryptSystemSettings` (`api/config_handlers.go`, no longer needs the user UUID), and the two mocks (`api/config_handlers_test.go`, `api/runtime_config_reader_adapter_test.go`).
3. **Backfill predicate unchanged.** `modified_by IS NOT NULL` keeps meaning "an operator set this value" because only `PUT /admin/settings/{key}` writes it. Shorten the known-gap comment in `internal/dbschema/system_setting_origin_backfill.go` (~105-120) to say the gap is closed by this issue.
4. **Tests.** Extend `TestSettingsService_ReEncryptAll_PreservesOrigin` to seed a row with `modified_by` and `modified_at` set and assert both are byte-identical after the pass, and that a row with NULL `modified_by` stays NULL. Ciphertext must still change.
5. **Oracle review** (`oracle-db-admin`) on the write path; expected trivial (`UpdateColumn` on a VARCHAR/CLOB value by primary key).

Not doing: a dedicated intent marker (option 3). `origin` already is that marker for every new write; this caller was the only leak.

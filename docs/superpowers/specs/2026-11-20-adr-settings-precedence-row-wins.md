# ADR: One settings precedence rule (admin-set row wins); callback URL and SAML enable are startup-only

- Status: Accepted
- Date: 2026-11-20
- Decision maker: Eric Fitzgerald (human decisions, 2026-11-20)
- Issues: #1168, #1167, #1169, #1163
- Amends: `2026-11-12-adr-settings-put-hot-only.md` (the "not explicitly set" clause)

## Context

`SettingsService` had two precedence rules for operational settings. `GetString`/`GetBool`/`GetInt` (`getConfigSetting`) let explicit config always win; `GetResolvedString` (`configOutranksRow`) let an admin-set (explicit) row outrank config. Since #1161 the admin settings reads follow `GetResolvedString`, so the admin API could show a value the server was not using (#1168).

`configWriteBlock` returned 409 on `PUT /admin/settings/{key}` whenever config set the key explicitly. Under the row-wins rule that would leave an admin-set row winning but impossible to change through the API.

`auth.oauth_callback_url` and `features.saml_enabled` were classified operational and read from the database at runtime, but their YAML paths (`auth.oauth.callback_url`, `auth.saml.enabled`) were unclassified, and SAML manager construction reads the struct at startup (#1167).

## Human decisions (Eric, 2026-11-20)

1. **Admin-set row wins (#1168).** Every read path uses the `configOutranksRow` table: an explicit (admin-set or dbtool-imported) row outranks config, explicit or not; a seeded default row loses to explicit config; with no usable row, the config layer (explicit or default) is used.
2. **PUT is allowed when config is explicit (amends the #1160 ADR).** For an Operational and Hot key, an explicit config value no longer blocks `PUT /admin/settings/{key}`. Static, bootstrap, provider-subtree and undefined keys still return 409. Eric chose this over keeping 409 (an admin-set row could then never be changed) and over 409-plus-DELETE.
3. **`auth.oauth_callback_url` and `features.saml_enabled` are startup-only (#1167).** Both come from config/env only. Their runtime database reads are removed, they are labeled Static and not operational, PUT returns 409, and the drift warning and `dbtool config import-legacy` leave them in the file. Existing database rows for these keys become inert. Eric chose this over keeping them runtime-editable.
4. **Fold #1169 and #1163 into `dev/1.21/settings-fixes`.** An unreadable `system_settings` row must not cause a 500; `timmy.*` mutability labels must match their runtime readers.
5. **The `timmy.*` embedding settings stay Hot (Eric, 2026-11-21).** `timmy.text_embedding_model`, `timmy.text_embedding_base_url` and `timmy.embedding_dimension` remain Hot. The chunk-embed worker must follow the same effective values (#1179). (Correction, 2026-11-21: the worker's vectors are not yet ingested, since the result consumer discards them and `timmy_embeddings` is written only by the monolith, so a mismatch does not break retrieval today; it would once worker vectors are used.)

## Human decisions (Eric, 2026-11-21)

6. **#1179 scope: transport only.** The monolith stamps the effective embedding profile (model, base URL, dimension) into the job envelope and the chunk-embed worker follows it, falling back to its env when the field is absent. Ingesting worker vectors, or removing the worker's embedding stage, is a separate issue. Eric chose this over adding ingestion now and over deferring.
7. **#1179 trust: worker-side allowlist.** The worker follows an envelope-supplied base URL only when its host is in a worker-configured allowlist; otherwise it fails the job with a clear error. The model name is followed freely. Eric chose this over following unconditionally (a rogue publisher could send the embedding API key anywhere) and over never following.
8. **#1180: the dev settings snapshot holds explicit rows only.** Export writes only explicit (admin-set) rows, with a format marker. Restore writes exactly those keys as explicit, replaces seeded rows, skips existing explicit rows, and refuses unmarked (old) snapshots. Existing stray explicit rows created by old restores are cleaned up once per cluster. Eric chose this over restoring as seeded and over a deny-list of pinned keys.
9. **#1181: an invalid row is unusable.** A row that fails semantic validation is treated like an unreadable row (#1169): no usable row, so the config layer wins, with a Warn naming the key only. Decision 1 therefore reads "a usable explicit row wins". The boot check no longer exits for an invalid row; an invalid env/YAML value still exits. Eric chose this over leaving the bad row in effect and over adding an API marker for ignored rows.

## Implementation choices (made during review, not human decisions; pending Eric's acknowledgment)

- Decision 7 is implemented more strictly than stated. An envelope embedding endpoint must use https unless it exactly matches the worker's env base URL (scheme, host and port). Endpoints containing URL credentials (userinfo) are rejected. The allowlist matches hostnames only, so any port is accepted; the worker's egress policy pins port 443. Rejections use reason `embedding_endpoint_not_allowed`.
- #1180 also fixed #1182: export nests keys by YAML path, so the three keys whose path differs from their setting key round-trip.

## Consequences

- The admin API shows the value the server uses for every operational key.
- After deploy, an existing admin-set row that env or the ConfigMap currently shadows starts winning. List such rows on each environment before deploying.
- The OAuth redirect_uri can no longer be overridden by a stale database row; the 2026-08-20 failure mode is gone.
- Enabling SAML always requires a config change and restart, which matches manager construction.
- `read_only` on the admin settings reads is derived from `configWriteBlock`: it is true exactly when `PUT /admin/settings/{key}` would return 409, whatever the setting's source. An env-pinned Hot key reads `read_only: false`; a Static key reads `read_only: true` even when a seeded row is shown. A UI that disables editing on the flag therefore agrees with the server.

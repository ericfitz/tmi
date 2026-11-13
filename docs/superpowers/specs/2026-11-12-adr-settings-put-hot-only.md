# ADR: Admin settings API writes only hot, non-explicit settings

- Status: Accepted
- Date: 2026-11-12
- Decision maker: Eric Fitzgerald (human decision, 2026-11-12)
- Issue: #1160

## Context

`PUT /admin/settings/{key}` returned 409 for every key that config knows about, even when neither the config file nor an env var set it. The read side (`SettingsService.getConfigSetting`) lets the DB win unless config is explicit, so runtime-togglable settings such as `timmy.enabled` could never be changed through the API.

Static settings are read from the config struct at boot, and no step loads DB values onto them. A DB write to a static setting would never take effect, not even after a restart. Provider-subtree settings (`auth.oauth.providers.*`, `auth.saml.providers.*`, `content_oauth.providers.*`) defined in config shadow DB rows, so writes to them would also be inert.

## Human decision (Eric, 2026-11-12)

For a key that config knows about, a PUT is allowed only when the setting is **Operational and Hot** and **not explicitly set** by the config file or an env var. Static, bootstrap, explicit and provider-subtree settings keep returning 409. Custom keys, which config doesn't know about, are unchanged. Eric chose this over loading DB values onto static settings at boot, and over accepting writes that never take effect.

## Implementation choices

- Hot/Static comes from the key's own `SettingDef` (`config.DefFor`), not from `MigratableSetting.Class`, which is projected from the registry and is Hot for almost every operational key (Ruling 15 defers unifying the two to Phase E).
- A defined key that is omitted from `GetMigratableSettings` (empty default) is judged by its def as well.
- `TestRuntimeWritableConfigSettings_ExactSet` pins the writable set (13 keys). `timmy.*` keys other than `timmy.enabled` are labeled Static in their defs but have DB readers; #1163 reconciles the labels.

## Consequences

- Admins can toggle hot settings at runtime unless an operator pinned them in config or env.
- `GET /admin/settings` can still show the config default after a successful PUT (#1161).

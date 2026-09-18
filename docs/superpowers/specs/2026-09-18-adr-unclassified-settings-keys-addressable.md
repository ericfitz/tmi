# ADR: Admin-created (unclassified) settings keys are addressable; only keys classified internal are hidden

- **Status:** Accepted (human decision by Eric Fitzgerald, 2026-09-18)
- **Deciders:** Eric Fitzgerald
- **Context recorded by:** Claude (tmi session, issue #916)
- **Related:** #916 (this decision), tmi-ux #942 (e2e cleanup of `e2e_admin_setting_*` rows)

## Context

`config.ClassificationFor(key)` returns the zero `ConfigClass` for a key the registry does not know.
The zero value has `Category = CategoryUnclassified` and `Visibility = VisibilityInternal`, so
"unknown" and "deliberately internal" were indistinguishable to any caller that looked only at
`Visibility`.

`GET /admin/settings/{key}` and `DELETE /admin/settings/{key}` did exactly that: they returned 404
whenever `Visibility == VisibilityInternal`. `PUT /admin/settings/{key}` has no such gate, and
`GET /admin/settings` returns every database row. An admin could therefore create a custom key (the
tmi-ux Admin -> Settings "Add setting" flow), see it in the list, and then never read it by key or
delete it. Leaked `e2e_admin_setting_*` rows on the shared Oracle ADB could not be removed through
the API.

## Options considered

- **A. Hide a key only when it is classified internal.** `hidden = Category != CategoryUnclassified
  && Visibility == VisibilityInternal`. Unclassified keys become readable and deletable by admins.
  Preferred by tmi-ux; keeps "Add setting" working.
- **B. Gate PUT so unclassified keys cannot be created.** Keeps the fail-closed default on every verb
  but removes custom settings and breaks the tmi-ux "Add setting" flow.

## Decision

**Option A** (Eric Fitzgerald, 2026-09-18).

## Consequences

- One helper, `isAPIHiddenSettingKey` in `api/config_handlers.go`, is the single by-key visibility
  rule for the admin settings endpoints. Bootstrap and internal-operational keys still 404.
- The fail-closed default is narrowed, deliberately: an unclassified key's value is shown unmasked to
  administrators. That is acceptable because such a key can only exist if an administrator wrote it
  through the same API. The public and non-admin views are unchanged: `filterByVisibility` still
  excludes unclassified keys.
- A key added to the registry later as internal becomes hidden again even if a database row exists.
- Not changed here, noted for follow-up: PUT has no visibility gate at the handler level, and the
  list endpoint does not filter database rows by classification.

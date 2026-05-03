# `x-tmi-authz` OpenAPI Vendor Extension

`x-tmi-authz` declares the authorization gates that every TMI API operation must
satisfy. It is enforced by `api/authz_middleware.go` at request time and by
`scripts/check-x-tmi-authz.py` at spec-validation time.

## Schema

```jsonc
"x-tmi-authz": {
  "ownership": "none" | "reader" | "writer" | "owner",  // required
  "roles":     ["admin" | "security_reviewer" | "automation" | "confidential_reviewer"],
  "public":    true | false,                            // default false
  "audit":     "required" | "optional"                  // default "required" for non-GET
}
```

## Field semantics

### `ownership` (required)

The minimum role the caller must hold on the parent resource.

- `none` — no resource-level check (used for `/admin/*`, `/me/*`, public endpoints, and
  global collections). The role list (and/or `public`) is the gate.
- `reader` / `writer` / `owner` — resource-hierarchical. Required for paths nested under
  `/threat_models/{id}/...` and similar. The middleware looks up the parent ACL.

### `roles` (optional, default `[]`)

Any one of the listed roles satisfies the gate. Roles are an **OR** list. Defined
roles in this slice:

- `admin` — member of the global Administrators group. Implemented by
  `api/auth_helpers.go::RequireAdministrator`.
- `automation` — member of either the `tmi-automation` or
  `embedding-automation` group. Implemented by
  `api/authz_middleware.go::checkAutomationRole`. The narrower
  `/automation/embeddings/*` gate (embedding-automation only) is layered
  via `api/automation_middleware.go::EmbeddingAutomationMiddleware`.

Future slices will register `security_reviewer` and `confidential_reviewer`
as the spec grows.

### `public` (optional, default `false`)

When `true`, the operation is unauthenticated. JWT middleware skips it via
`PublicPathChecker`; `AuthzMiddleware` returns immediately. Public operations
**must** have `ownership: none` and `roles: []` — the validator enforces this.

### `audit` (optional)

Informational in slice 1. Slice 8 wires audit-emission enforcement.

## Examples

```jsonc
// Public OAuth metadata endpoint
"x-tmi-authz": { "ownership": "none", "public": true }

// Admin-only endpoint
"x-tmi-authz": { "ownership": "none", "roles": ["admin"] }

// Resource-hierarchical write
"x-tmi-authz": { "ownership": "writer" }

// Resource-hierarchical write that additionally requires the security_reviewer role
"x-tmi-authz": { "ownership": "writer", "roles": ["security_reviewer"] }
```

## Adding a new endpoint

Every operation in `api-schema/tmi-openapi.json` MUST carry `x-tmi-authz`.
`make validate-openapi` fails the build on any operation lacking the
extension (default-deny since #371). Steps for a new endpoint:

1. Pick the URL pattern per CLAUDE.md ("URL Pattern Guidelines"). The
   pattern usually narrows the authz model — for example,
   resource-hierarchical paths under `/threat_models/{id}/...` use the
   `reader`/`writer`/`owner` ownership levels because the middleware
   resolves the parent threat-model ACL.
2. Pick `ownership`. Most paths fall into one of:
   - `none` for global collections, `/admin/*`, `/me/*`, public, and
     workflow paths whose role check is enforced inside the handler
     (team-membership for `/projects`, subject-self for `/me/*`,
     HasAccess for `/triage/*`).
   - `reader` for any GET on a resource nested under a threat model.
   - `writer` for POST/PUT/PATCH on a nested resource (and for DELETE
     on most sub-resources — top-level threat-model and diagram DELETE
     remain `owner`).
   - `owner` for top-level threat-model DELETE, the various `/restore`
     endpoints, and the audit-trail rollback.
3. Pick `roles` if the route has a role gate that crosses ownership
   (e.g. `[admin]` for `/admin/*`, `[automation]` for `/automation/*`).
4. Pick `public: true` only if the endpoint is genuinely unauthenticated
   per RFC. Pair with `ownership: none` and no `roles`.
5. Run `make validate-openapi` and `make lint` before committing —
   the validator will catch missing or malformed declarations and the
   companion lint rule (`scripts/check-no-adhoc-authz.py`) will fail
   if you add a redundant role check inside the handler.

## Migration history

The `x-tmi-authz` migration shipped in eight slices:

| # | Issue | Coverage |
| - | ----- | -------- |
| 1 | #341 | Foundation, `/admin/*`, `/.well-known/*`, `/oauth2/*`, `/saml/*`, public root |
| 2 | #365 | `/threat_models` top-level + `/diagrams` top-level + `/ws/ticket` |
| 3 | #366 | `/threat_models/{id}/*` nested sub-resources (threats, documents, notes, assets, repositories, audit_trail, metadata) |
| 4 | #367 | `/me` and `/me/*` user-scoped |
| 5 | #368 | `/addons*` + `/automation/embeddings/*` (introduces the `automation` role gate) |
| 6 | #369 | `/threat_models/{id}/chat/sessions/*` Timmy chat endpoints |
| 7 | #370 | `/intake/*`, `/triage/*`, `/projects*` workflow + cross-cutting |
| 8 | #371 | Closes the long tail (`/teams/*`, `/webhook-deliveries/{id}`), flips the validator to default-deny, sweeps redundant route-level checks from handlers, adds the `check-no-adhoc-authz.py` lint rule |

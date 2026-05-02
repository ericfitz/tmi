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

Future slices will register `security_reviewer`, `automation`,
`confidential_reviewer` as the spec grows.

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

Every operation in `api-schema/tmi-openapi.json` must carry `x-tmi-authz` once
slice 8 (#371) lands. Until then, the prefix allowlist in
`scripts/check-x-tmi-authz.py` controls which operations are checked. Add new
endpoints with the extension from day one — see the examples above.

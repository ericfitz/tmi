# ADR: `direct_write` flag for client credentials (automation write access under T18)

- **Status:** Accepted (human decision by Eric Fitzgerald, 2026-09-06)
- **Deciders:** Eric Fitzgerald
- **Context recorded by:** Claude (tmi session `rusty-jackal-30`, xfa thread #14 → #56)
- **Related:** #358 (T18 confused deputy), #855 (unemitted repository/metadata events), docs/THREAT_MODEL.md §4/§6

## Context

### How service-account tokens work today

A `client_credentials` token (RFC 6749 §4.4) has **no identity of its own**. Its JWT subject is
`sa:{credential_id}:{owner_provider_user_id}`, and `cmd/server/jwt_auth.go` sets `userID` to the
**owner's** provider user id. Every role and ownership check therefore runs as the owning user.
ACL principals are only `user` and `group` (`Principal.principal_type`), so a service account
cannot hold a role directly; it inherits whatever the owning user holds.

### T18 and the invoker-only gate (#358)

Addons are registered by administrators and invoked by any authenticated user. If an addon writes
back to TMI with its own client credential, it acts with the *addon owner's* authority (typically an
administrator with owner-level access to the threat model). A low-privilege invoker could thus cause
writes they could not perform themselves: a confused-deputy escalation (threat T18).

The mitigation shipped for #358 has two parts:

1. Every threat-model write route carries `x-tmi-authz.subject_authority: invoker` (94 operations
   in the spec). `enforceSubjectAuthority` in `api/authz_middleware.go` rejects **every**
   service-account token on those routes with 403, *before* the role/ownership gates run.
2. Addon write-backs use a scoped delegation JWT minted per `addon.invoked` delivery
   (`X-TMI-Delegation-Token`, `auth/delegation_token.go`), whose subject is the invoking user.

### Why the gate is too coarse

The gate cannot distinguish "addon deputized by a stranger" from "the owner's own automation acting
for the owner". Holding writer or owner role on the threat model does not help, because the rejection
happens before the ACL is consulted. In practice this blocks the integration model TMI is designed
for: an automation (here tmi-tf-wh) triggered by a webhook that then manipulates threat models via the
REST API with its own credentials.

### The delegation token does not fit long-running automation

`DelegationTokenTTL` is **60 seconds**, the token is bound to a single delivery, and there is no
refresh or re-mint endpoint. Addon invocations themselves expire after roughly 15 minutes and are
quota-limited (3 concurrent, 10 per hour per user). A tmi-tf-wh analysis runs 10–40 minutes, so the
delegation path cannot carry its write-back even if tmi-tf-wh were rewritten as an addon.

### The intended integration model (Eric's decision)

Webhook authentication and API authentication are separate protocols and must stay separate. The
webhook is **only a trigger**. The automation has its own **dedicated, non-administrator TMI user**
("automation account"), a member of the `tmi-automation` group, which is granted writer (or owner)
on the threat models it may modify. Client credentials are minted for that user and stored as a
secret; the automation uses `client_credentials` against the TMI API to read and modify threat
models directly, acting as that user with exactly its granted rights. This already fits TMI's
identity model; only the T18 gate stands in the way.

## Decision

Add an **opt-in, per-credential `direct_write` flag** to client credentials.

- Stored on the client credential (model column), exposed as an optional boolean on the
  client-credential create/edit API (`/me/client_credentials`, `/admin/users/{id}/client_credentials`),
  and carried as a claim in the issued JWT.
- When a service-account token carries `direct_write: true`, `enforceSubjectAuthority` does **not**
  reject it on `subject_authority: invoker` routes; the request falls through to the normal role and
  ownership gates and is authorized exactly like the owning user.
- Every write performed under a `direct_write` token is audit-tagged with the credential id.
- The flag is **refused** (400/403) on credentials whose owning user is a member of the
  Administrators group, so the #358 scenario (admin-owned addon credential) stays closed.
- Admin routes remain denied to all service-account tokens; the flag does not touch that rule.

### Compatibility

Non-breaking API addition: the field is optional; absent on create or edit means `false`, which is
byte-for-byte today's behavior. Minor version bump of the OpenAPI spec.

## Alternatives considered

- **Remove `subject_authority: invoker` from the routes and rely on ACL alone.** Simplest, but
  reopens #358 for administrator-owned addon credentials. Rejected.
- **Extend the delegation token (longer TTL or a re-mint endpoint keyed on delivery id).** Keeps
  T18 fully intact but forces automations into the addon protocol and its quotas, and mixes webhook
  and API auth, which Eric explicitly rejected. Not pursued now; may still be worth doing for addons.
- **Service accounts as first-class ACL principals** (`principal_type: service_account`). Cleanest
  long-term model, but a larger change touching ACLs, groups, and the UI. Deferred.

## Consequences

- tmi-tf-wh keeps its current design (webhook trigger, `client_credentials` for API calls) and needs
  no protocol change; it only needs credentials minted with `direct_write: true` for its bot user.
- Operators must create a non-admin bot user per automation and grant it roles explicitly; the flag
  cannot be used to widen an administrator's credential.
- The audit trail distinguishes automation writes from interactive writes by credential id.
- tmi-ux gains a `direct_write` control on the automation-account (client credential) UI.

## Follow-ups

- TMI: implement the flag (issue filed from this ADR).
- tmi-ux: expose the flag in the client-credential UI (issue filed from this ADR).
- After it ships: create the tmi-tf-wh bot user in `tmi-automation` with writer on the demo threat
  model, mint `direct_write` credentials, rotate `~/.keys/TMI_TF_WH_CLIENT_ID` / `_SECRET`.

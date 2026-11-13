# ADR: First-user promotion grants Administrators only

- Status: Accepted
- Date: 2026-11-12
- Decision maker: Eric Fitzgerald (human decision, 2026-11-12)
- Issue: #1156

## Context

With `auth.auto_promote_first_user` enabled, the first user to authenticate while the Administrators group is empty was added to **both** Administrators and Security Reviewers (`cmd/server/jwt_auth.go`, member note "Auto-promoted as first security reviewer"). This happened regardless of `auth.everyone_is_a_reviewer`.

That grant broke the reviewer-group triage model (ADR 2026-10-17, #1100) on fresh databases: the integration harness's admin became the first user, so it was a reviewer and could triage its own survey responses (`TestSurveyResponseWorkflow`, #1156).

## Human decision (Eric, 2026-11-12)

Making the first user a security reviewer, when `everyone_is_a_reviewer` is not true, is a bug. First-user promotion grants **Administrators membership only**. Security Reviewers membership comes only from `auth.everyone_is_a_reviewer=true` or an explicit grant.

Integration tests follow the project convention: the harness authenticates `charlie` first, so `charlie` is the (auto-promoted) administrator, and other users are seeded after `charlie`. Tests that need a non-reviewer actor use an ordinary user (`alice`, `bob`, ...).

## Consequences

- A fresh install's first user is an administrator but not a security reviewer. Administrators are not reviewers by virtue of the role (ADR 2026-10-17), so they must add themselves (or others) to Security Reviewers to triage, unless `everyone_is_a_reviewer` is enabled.
- Existing installs are unaffected: memberships already granted by the old behavior stay; nothing removes them.
- `auth.everyone_is_a_reviewer` semantics are unchanged.

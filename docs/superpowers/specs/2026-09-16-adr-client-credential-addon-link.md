# ADR: `addon_id` link on client credentials (self-delivery suppression for direct_write automations)

- **Status:** Accepted (human decision by Eric Fitzgerald, 2026-09-16)
- **Deciders:** Eric Fitzgerald
- **Context recorded by:** Claude (tmi session, 2026-09-16)
- **Related:** #883, #876 (delegation-token self-delivery suppression), #856 / ADR 2026-09-06 (`direct_write`), T18 (#358)

## Context

#876 stopped delivering an event back to the addon whose delegation-token write-back caused it:
the delegation JWT names the addon, `cmd/server/jwt_auth.go` tags the request context with that
addon id, and `api/webhook_event_consumer.go` skips the addon's own subscription.

Automations built on the #856 model do not use delegation tokens. tmi-tf-wh's worker writes with a
`client_credentials` token from a `direct_write` credential owned by a non-admin bot user. Those
requests carry no addon identity, so #876 cannot see them, and every write echoes back as a
`metadata.updated` delivery to the addon's subscription (verified on api.tmi.dev 2026-09-16: three
unsuppressed deliveries to subscription 25603436 during one invocation).

## Options considered

1. **Explicit link on the credential** (chosen): a nullable `addon_id` column on
   `client_credentials`, settable at creation, pointing at the addon (the addon's `webhook_id`
   resolves to the subscription). Token issuance copies it into a `tmi_addon_id` claim; the JWT
   middleware feeds it to the same context key #876 uses; the consumer is unchanged.
2. **Ownership rule** (rejected): suppress when the credential owner owns the subscription. The bot
   user would need ownership of an admin-managed subscription (or admin rights), which contradicts
   the T18 separation `direct_write` exists to preserve.
3. **URL or timing heuristics** (rejected): guessing that a write "came from" an in-flight delivery
   is unreliable and unexplainable.
4. **Do nothing** (rejected): consumers must keep an allow-list to ignore their own echoes and still
   pay the delivery cost.

## Decision

Option 1. Rules:

- `addon_id` is accepted on `POST /me/client_credentials`, `POST /admin/users/{user_id}/client_credentials`
  and `POST /admin/automation_accounts`; returned on `ClientCredentialResponse` and `ClientCredentialInfo`.
- It requires `direct_write=true` and an existing addon; otherwise 400 `invalid_request`. Only a
  direct_write token can cause events, so the link is meaningless without it.
- Not a foreign key. Deleting the addon leaves the credential valid; the link just stops suppressing.
- The claim is only minted when the `direct_write` claim is granted (same fail-closed admin check).

## Consequences

- tmi-tf-wh must recreate credential `e87017a6` with `addon_id=fed11e3a` (the secret is shown once).
- Residual: any user with `direct_write` may link to any existing addon and thereby stop that addon's
  subscription from receiving events caused by *their own* writes. That is a narrow integrity gap
  (suppression, not access) and was accepted rather than adding an ownership check.

# Delegated Content Providers

> **This file is wiki content awaiting manual copy to the TMI wiki (https://github.com/ericfitz/tmi/wiki). Do not update here; update the wiki.** Once copied, delete this file.

## What delegated content providers are

TMI's Timmy workflow reads external documents (Google Drive, Confluence, OneDrive, etc.) when indexing threat-model context. Two access models exist, depending on how the operator trusts TMI to reach the provider:

| Model | How credentials flow | Typical providers |
|-------|----------------------|-------------------|
| **Service content provider** | Operator configures one bot/service account. Users explicitly share docs with it. | Google Drive, OneDrive (planned) |
| **Delegated content provider** | Each user links their own account once via OAuth. TMI stores an encrypted refresh token and makes API calls on the user's behalf. | Confluence (planned), Google Workspace delegated (planned) |

Service providers are described on a separate wiki page. This page covers the **delegated** model and its infrastructure.

## Operator prerequisites

Before enabling any delegated content provider, the operator must configure the token-encryption key:

| Variable | Purpose |
|----------|---------|
| `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` | 32-byte hex (64 chars) AES-256-GCM key. Separate from settings-encryption key. Required when any delegated content provider is enabled. |

If a delegated provider is enabled (`TMI_CONTENT_OAUTH_PROVIDERS_*_ENABLED=true`) and the key is missing, the server refuses to start.

Generate a random key: `openssl rand -hex 32`. Store securely (e.g., secrets manager). Rotating this key invalidates all existing user content tokens — users will need to re-link.

## Per-provider configuration

Delegated providers are configured under `content_oauth.providers.<id>` in YAML, or via environment variables:

```yaml
content_oauth:
  callback_url: "https://tmi.example.com/oauth2/content_callback"
  allowed_client_callbacks:
    - "https://app.example.com/content-linked"
  providers:
    confluence:
      enabled: true
      client_id: "atlassian-client-id"
      client_secret: "atlassian-client-secret"
      auth_url: "https://auth.atlassian.com/authorize"
      token_url: "https://auth.atlassian.com/oauth/token"
      userinfo_url: "https://api.atlassian.com/me"
      revocation_url: ""     # Atlassian does not expose RFC 7009; leave empty
      required_scopes: ["read:confluence-content.all"]
```

Equivalent env vars:

| Variable | Purpose |
|----------|---------|
| `TMI_CONTENT_OAUTH_CALLBACK_URL` | TMI's callback URL. Register this with the provider. |
| `TMI_CONTENT_OAUTH_ALLOWED_CLIENT_CALLBACKS` | Comma-separated allow-list for the `client_callback` URLs that the UI supplies to `POST /me/content_tokens/{id}/authorize`. |
| `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_ENABLED` | `true` to enable this provider. |
| `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_CLIENT_ID` | OAuth client id registered with the provider. |
| `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_CLIENT_SECRET` | OAuth client secret. |
| `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_AUTH_URL` | Provider's authorization endpoint. |
| `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_TOKEN_URL` | Provider's token endpoint. |
| `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_USERINFO_URL` | Optional. Used to fetch a human-readable account label at link time. |
| `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_REVOCATION_URL` | Optional (RFC 7009). If provided, TMI revokes at the provider on disconnect. |
| `TMI_CONTENT_OAUTH_PROVIDERS_{ID}_REQUIRED_SCOPES` | Space-separated scopes. |

## API endpoints

| Endpoint | Purpose |
|----------|---------|
| `GET /me/content_tokens` | List the caller's linked providers. No secrets. |
| `POST /me/content_tokens/{provider_id}/authorize` | Start the link flow. Body: `{"client_callback": "https://..."}`. Returns `{authorization_url, expires_at}`. |
| `DELETE /me/content_tokens/{provider_id}` | Unlink a provider. Revokes at the provider (if revocation URL is configured), then deletes the row. Idempotent. |
| `GET /oauth2/content_callback` | Public. Provider redirects here after user consent. Exchanges code for tokens, stores encrypted, 302s back to the `client_callback` with `status=success` or `status=error`. |
| `GET /admin/users/{user_id}/content_tokens` | Admin: list a target user's links. |
| `DELETE /admin/users/{user_id}/content_tokens/{provider_id}` | Admin: revoke + delete for a target user. |

When no delegated providers are configured, these endpoints return **503 Service Unavailable** — this is the expected state for a default TMI deployment.

## Token lifecycle

- **Link:** user goes through the authorize → callback flow once per provider. Access + refresh tokens are encrypted with `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` and stored in `user_content_tokens`.
- **Fetch:** delegated sources look up the user's token on each content fetch. If expired (with a 30-second skew), TMI refreshes lazily and serializes concurrent refreshes via `SELECT ... FOR UPDATE`.
- **Refresh failure:** 4xx from the provider's token endpoint flips the row to `status=failed_refresh`. Subsequent fetches return `ErrAuthRequired` without calling the provider; the user must re-link.
- **User deletion:** TMI sweeps the user's content tokens and attempts revocation at each provider before the database cascade removes the rows. Revocation failures are logged but do not block deletion.

## When to use what

- **Building an automation that reads a few docs on behalf of a user** → delegated provider.
- **Running a bot/service that operates on docs the user has shared with it** → service provider (see the separate wiki page).
- **Integrating with an SSO identity provider for logging users into TMI itself** → that's user-auth OAuth, a different subsystem under `oauth.providers.*`. Do not conflate it with `content_oauth.providers.*`.

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| Server refuses to start with a `TMI_CONTENT_TOKEN_ENCRYPTION_KEY` error | Enabled a provider without setting the key. Generate one or disable the provider. |
| `GET /me/content_tokens` returns 503 | No delegated providers are configured. Expected on a fresh deployment. |
| Callback redirects with `error=client_callback_not_allowed` | The `client_callback` URL you supplied is not in `content_oauth.allowed_client_callbacks`. Add it (supports trailing-`*` wildcards). |
| Callback redirects with `error=invalid_state` or renders "missing_state" | The OAuth state TTL is 10 minutes. If the user took longer, they need to restart. Possible: the Redis instance was reset between authorize and callback. |
| User sees "account needs reconnecting" | Row has `status=failed_refresh` — provider revoked or invalidated the token. User reconnects via the link flow. |

## Related

- Content provider architecture overview (service + delegated model separation): see the Content Providers wiki page.
- Spec for this subsystem: `docs/superpowers/specs/2026-04-18-delegated-content-provider-infrastructure-design.md` (in the repo).
- Tracking issue: ericfitz/tmi#249.

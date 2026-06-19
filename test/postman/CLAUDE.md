# Postman/Newman API Tests

## Running tests (always via make targets)

- All collections: `make test-api`
- One collection: `make test-api-collection COLLECTION=comprehensive-test-collection`
- List collections: `make test-api-list`

These wrap `scripts/run-api-tests.py` (→ `run-tests.sh` / `run-postman-collection.sh`).
Never call `newman run` directly — per the root CLAUDE.md, always use make targets.

## Authentication

Tests authenticate via the TMI OAuth provider through the OAuth callback stub
(port 8079) using the authorization-code/PKCE flow — there is no implicit flow.

- `run-tests.sh`: automated flow — `POST /flows/start` then poll `GET /flows/{id}`
  for `tokens.access_token`, for users alice/bob/charlie/diana.
- `multi-user-auth.js`: `GET /oauth2/authorize?idp=tmi&login_hint=X&client_callback=<stub>/&scope=openid`,
  then `GET /creds?userid=X`.

Start the stub first: `make start-oauth-stub`.

## Order-dependent workflows

Many APIs depend on prior calls (create parent before child, create before delete).
These sequences are documented in `api-schema/api-workflows.json`.

## Gotchas

- Use IPv4 (`127.0.0.1`), not `localhost` — avoids `ECONNREFUSED ::1` IPv6 issues with newman.
- Bearer auth uses the `{{access_token}}` collection variable; check overriding requests don't use a stale variable name.
- 401 "invalid number of segments": token variable mismatch / empty token.
- 404 from stub `/creds`: user not authenticated yet, token not saved.
- The `unauthorized-tests-collection.json` runs first, before authentication, to verify 401 behavior.

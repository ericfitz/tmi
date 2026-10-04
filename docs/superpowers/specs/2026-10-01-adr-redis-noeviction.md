# ADR: Redis noeviction with a 384mb cap

- Status: Accepted
- Date: 2026-10-01
- Decision maker: Eric Fitzgerald (human decision)
- Issue: #1008 (PR #1019)

## Context

Redis ran with `allkeys-lru` under `--maxmemory 160mb`. Under memory pressure it could evict `blacklist:token:*` revocation entries, which would silently re-validate revoked tokens. `volatile-*` policies don't help, because blacklist entries carry TTLs too. A separate Redis DB number doesn't isolate eviction either, since the policy applies to the whole instance.

## Options considered

- `noeviction`.
- `noeviction` with a higher cap.
- A separate Redis instance for revocations.
- Keep `allkeys-lru`.

## Decision

**`noeviction`, with the cap raised to 384mb and a 640Mi container limit.** A revoke that cannot be stored returns 503 (`/oauth2/revoke`, `DELETE /me/client_credentials/{id}`), so revocation fails closed.

## Consequences

- At the cap, every Redis write fails until keys expire, including logins, sessions, OAuth state and rate-limit counters. The service degrades loudly instead of silently weakening revocation.
- 384mb keeps an AOF rewrite (about 2× the dataset) inside the existing 1Gi PVC. A higher cap needs PVC expansion, which docker-desktop's hostpath class doesn't support.

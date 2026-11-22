# ADR: threat_model_framework is an enum on every write

Status: accepted (human decision, Eric Fitzgerald, 2026-11-21)

## Context

The OpenAPI spec allowed any `^[A-Za-z0-9_-]*$` token up to 30 characters for `threat_model_framework`, and create did not validate it. The ThreatModel GORM `BeforeUpdate` hook enforced CIA, STRIDE, LINDDUN, DIE and PLOT4ai. CATS (run 20261121T201842Z) created a threat model with a random framework; every later save of that row failed validation, so `POST /me/transfer` returned 500 for its owner. tmi-ux offers only the five values.

## Decision

`threat_model_framework` is an enum (CIA, STRIDE, LINDDUN, DIE, PLOT4ai) enforced on create, PUT and PATCH, with 400 for any other non-empty value. Omitting it keeps the existing default.

## Alternatives considered

- Free-form token: allows custom frameworks without code changes, but the client cannot render unknown ones.
- Database-backed configurable list: most flexible, most work.

## Consequences

- The spec contract tightens (a breaking schema change).
- Rows already holding another value (dev clusters seeded by CATS) need a one-time cleanup; ownership transfer no longer re-validates unrelated stored fields, so such rows do not block transfer.

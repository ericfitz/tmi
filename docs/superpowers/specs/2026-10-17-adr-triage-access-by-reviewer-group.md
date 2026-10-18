# ADR: Restrict survey triage to reviewer groups by confidentiality

- Status: Accepted
- Date: 2026-10-17
- Decision maker: Eric Fitzgerald (human decision, 2026-10-17)
- Issue: #1100

## Context

The `/triage/survey_responses/*` endpoints authorized callers with the per-response ACL (`survey_response_access`). Response creation seeds that ACL with the submitter as `owner`, so a submitter could read and change their own response through the triage endpoints (including patching status and creating a threat model from it). The submitter is supposed to reach their response through the intake endpoints only.

## Human decision (Eric, 2026-10-17)

Triage endpoints allow only members of **Security Reviewers** to see or change **non-confidential** survey responses, and only members of **Confidential Project Reviewers** to see or change **confidential** ones, regardless of ownership. Ownership grants no triage access. The submitter keeps access through the intake endpoints only.

## Implementation choices (controller rulings under that decision)

- **Per-id denial is 404 `not_found`**, the existing non-disclosure convention (T5, #357), so a denied caller cannot tell an existing response from a missing one. The same 404 covers a missing response.
- **List returns 200 with an empty page** for a caller in neither reviewer group. A caller in one group sees only responses with the matching `is_confidential`; a caller in both sees all. The confidentiality filter is applied in the store query (GORM `ColumnMap` predicate, no dialect-specific SQL), so totals and pagination stay correct and do not leak counts.
- **No administrator bypass.** Administrators are not reviewers by virtue of the role; they need the group membership like anyone else.
- **ACL seeding is unchanged.** The submitter's owner row in `survey_response_access` stays because intake depends on it. Triage simply stops consulting that table.
- **Membership is resolved per request** with `IsGroupMemberFromContext`, so IdP-mapped and nested group membership counts, unlike the store's `HasAccess`, which reads direct `group_members` rows only.
- **No OpenAPI change.** 404 and 403 are already documented on the per-id triage operations, and the list returns 200.
- **Triage sub-resources** (metadata, triage notes) use the same guard. Triage notes are shared between the intake (read-only) and triage routes; the triage routes mark the request as verified so the shared handler skips the intake-oriented ACL check, while the intake routes keep the ACL check.

## Why a guard rather than an `x-tmi-authz` role

A static role gate on the route cannot see the per-row `is_confidential` value, so it cannot choose between the two groups. A gate allowing either group would let a Security Reviewers member reach confidential responses. The handler-level guard `RequireTriageAccess` loads the row and checks membership in `triageReviewerGroup(is_confidential)`.

## Consequences

- A submitter who is not a reviewer gets 404 on every triage per-id route and an empty triage list.
- A reviewer no longer needs an ACL row on a response to triage it, and an ACL row alone no longer grants triage.
- Existing tests that encoded owner-based triage access were updated to authenticate as reviewer-group members.

## Intake status writes (controller ruling that follows from Eric's decision)

Restricting triage endpoints is not enough if the submitter can reach the same outcome through intake: `PATCH /intake/survey_responses/{id}` previously accepted any valid status value, so a submitter could move their own response to `ready_for_review` or `review_created` and bypass triage. Intake writes may now set status only to `draft` or `submitted`; any other target returns 400 `invalid_input` from one shared helper, `validateIntakeStatusTarget`. `PUT` and create carry no status field, so PATCH is the only intake path affected. Triage status transitions are unchanged.

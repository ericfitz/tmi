# Design: Confidential Project Reviewers Well-Known Group

**Issue**: #187
**Date**: 2026-03-16

## Summary

Add a well-known "Confidential Project Reviewers" group to the server, following the same pattern as the existing `security-reviewers` and `administrators` groups. When a survey response or threat model is created with `is_confidential=true`, automatically add this group instead of `security-reviewers`.

## Group Definition

| Field | Value |
|-------|-------|
| group_name | `confidential-project-reviewers` |
| display name | `Confidential Project Reviewers` |
| UUID | `00000000-0000-0000-0000-000000000003` |
| provider | `*` (provider-independent) |

## Auto-Assignment Logic

On resource creation:
- `is_confidential=false` (or nil): add `security-reviewers` group with owner role
- `is_confidential=true`: add `confidential-project-reviewers` group with owner role

Applies to both survey responses and threat models.

**Note on existing behavior**: Survey responses already auto-add `security-reviewers` when `is_confidential=false`. Threat models do NOT currently auto-add any reviewer group — adding `security-reviewers` for non-confidential threat models is **new behavior** introduced by this feature.

**nil handling**: `is_confidential=nil` is treated as `false` (add `security-reviewers`), consistent with the existing survey response pattern.

**Existing data**: This is forward-looking only. Existing threat models and survey responses are not retroactively updated. No data migration.

## Changes by File

### 1. `api/auth_utils.go`
Add constants and helpers mirroring the SecurityReviewers pattern:
- `ConfidentialProjectReviewersGroup = "confidential-project-reviewers"`
- `ConfidentialProjectReviewersGroupUUID = "00000000-0000-0000-0000-000000000003"`
- `ConfidentialProjectReviewersAuthorization()` — returns Authorization entry with `AuthorizationRoleOwner`
- `IsConfidentialProjectReviewersGroup()` — predicate for checking auth entries

Update existing comments on `SecurityReviewersGroup` and `SecurityReviewersAuthorization()` to reflect that they now also apply to threat models (non-confidential).

### 2. `api/validation/validators.go`
- Add `ConfidentialProjectReviewersGroupUUID = "00000000-0000-0000-0000-000000000003"` alongside existing UUIDs
- Add the new UUID to `BuiltInGroupUUIDs` slice (protects group from deletion/renaming)

### 3. `api/group_membership.go`
Add `GroupConfidentialProjectReviewers` var of type `BuiltInGroup`.

### 4. `api/seed/seed.go`
Add `seedConfidentialProjectReviewersGroup()` following the idempotent `FirstOrCreate` pattern. Call from `SeedDatabase()`.

### 5. `api/survey_response_store_gorm.go`
Modify the create path: add an `else` branch so that when `is_confidential=true`, call `ensureConfidentialProjectReviewersGroup()` and add the group access entry. This mirrors the existing `ensureSecurityReviewersGroup()` path for non-confidential responses. Follow the same pattern using `string(AuthorizationRoleOwner)` for the role.

### 6. `api/threat_model_handlers.go`
Modify `CreateThreatModel` handler: add conditional logic in the handler layer (consistent with how `ApplySecurityReviewerRule` already works there for the individual reviewer user). When `is_confidential=true`, append `ConfidentialProjectReviewersAuthorization()` to the authorization list. When false/nil, append `SecurityReviewersAuthorization()`. The group is guaranteed to exist via seed.go (no `ensure*` pattern needed in the handler).

### 7. Tests
- Unit tests for new helper functions (`ConfidentialProjectReviewersAuthorization()`, `IsConfidentialProjectReviewersGroup()`)
- Unit tests for conditional auto-assignment in survey response creation (both confidential and non-confidential paths)
- Unit tests for conditional auto-assignment in threat model creation (both confidential and non-confidential paths)
- Seed test: verify the new group is created by `SeedDatabase()`
- Validation test: verify `IsBuiltInGroup()` returns true for the new UUID

## Out of Scope

- No OpenAPI schema changes (group is a server-side fixture)
- No data migration for existing resources
- `is_confidential` remains immutable after creation (already enforced)

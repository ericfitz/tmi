# Design: Add "name" query parameter to GET /admin/users

**Issue**: [#182](https://github.com/ericfitz/tmi/issues/182)
**Date**: 2026-03-15
**Status**: Approved

## Summary

Add a `name` URL query parameter to `GET /admin/users` for case-insensitive substring filtering by user display name. Also add `name` as a valid `sort_by` option. This supports the admin UI in tmi-ux which needs to search/filter users by name.

## Design

The implementation follows the established pattern used by the existing `email` query parameter.

### 1. OpenAPI Spec (`api-schema/tmi-openapi.json`)

- Reuse existing `NameQueryParam` from `components/parameters` (already defined for threat model name filtering with `maxLength: 256`, `pattern: ^[^\x00-\x1F]*$`). Update its description to be generic: `"Filter by name (case-insensitive substring match)"`.
- Add `{ "$ref": "#/components/parameters/NameQueryParam" }` to the `GET /admin/users` parameters list (after `EmailQueryParam`)
- Add `"name"` to `SortByQueryParam` enum: `["created_at", "last_login", "email", "name"]`

### 2. UserFilter struct (`api/user_store.go`)

Add `Name string` field with comment matching email's style:

```go
Name string // Case-insensitive ILIKE %name%
```

### 3. GormUserStore (`api/user_store_gorm.go`)

Add name filter in both `List()` and `Count()`:

```go
if filter.Name != "" {
    query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+filter.Name+"%")
}
```

Add `"name"` to the valid `sortBy` switch case in `List()`.

Update `SortBy` comment in `UserFilter` struct to include `name`.

### 4. Handler (`api/admin_user_handlers.go`)

Extract `params.Name` and pass to `UserFilter.Name`, identical to email handling:

```go
name := ""
if params.Name != nil {
    name = *params.Name
}
```

### 5. Tests (`api/admin_user_handlers_test.go`)

Add test cases for:
- Filtering by name substring match
- Case-insensitive name matching
- Combined name + email filtering
- Sorting by name

## Non-goals

- No full-text search or fuzzy matching
- No new database indexes (the `name` column is `varchar(256)` and admin user listing is low-volume)

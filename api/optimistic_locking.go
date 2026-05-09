// Package api implements the TMI HTTP API.
//
// optimistic_locking.go provides shared helpers for the If-Match / version
// optimistic-locking contract introduced for T14 (#385). Mutable top-level
// entities (threat models, diagrams, projects, teams, assets, threats,
// documents, surveys) carry an integer Version column. PUT/PATCH callers
// pass the expected current version via the If-Match header (preferred) or
// a "version" body field (fallback). The repository layer then issues a
// versioned UPDATE; on a version mismatch handlers return 409 Conflict.
//
// Rollout policy this release:
//   - If neither If-Match nor body version is provided, the write proceeds
//     but the response carries a Deprecation/Warning header.
//   - When config RequireIfMatch is true (planned for the next release),
//     missing If-Match returns 428 Precondition Required.
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// requireIfMatchFlag is the package-level mirror of config.Server.RequireIfMatch.
// Set once during server bootstrap via SetRequireIfMatch. atomic.Bool keeps
// reads cheap on the hot path of every PUT/PATCH.
var requireIfMatchFlag atomic.Bool

// SetRequireIfMatch updates the optimistic-locking enforcement flag. Called
// once during server initialization from the loaded config.
func SetRequireIfMatch(v bool) {
	requireIfMatchFlag.Store(v)
}

// RequireIfMatch reports whether missing If-Match should hard-fail with 428.
func RequireIfMatch() bool {
	return requireIfMatchFlag.Load()
}

// VersionDeprecationMessage is emitted in the Warning response header when a
// caller omits both If-Match and the body version field. Per RFC 7234 the
// Warning header is "299 - <message>".
const VersionDeprecationMessage = `299 - "If-Match header (or body 'version') is required for PUT/PATCH; future releases will return 428 Precondition Required"`

// VersionDeprecationLink is emitted in the Deprecation header (RFC 8594) so
// clients have a stable signal they can scan for.
const VersionDeprecationLink = `true`

// ParseIfMatchHeader extracts a non-negative integer version from the
// If-Match request header. Returns (version, true, nil) on success,
// (0, false, nil) if the header is absent, or (0, true, err) if the header
// is present but malformed.
//
// Per RFC 7232 If-Match values are quoted ETags. We accept either a bare
// integer ("If-Match: 5") or a quoted integer (`If-Match: "5"`) for client
// convenience. The "*" wildcard form is intentionally rejected for now —
// callers should send an explicit version.
func ParseIfMatchHeader(c *gin.Context) (int, bool, error) {
	raw := strings.TrimSpace(c.GetHeader("If-Match"))
	if raw == "" {
		return 0, false, nil
	}
	// Strip surrounding quotes (ETag format) and weak prefix.
	v := strings.TrimPrefix(raw, "W/")
	v = strings.Trim(v, `"`)
	v = strings.TrimSpace(v)
	if v == "*" {
		return 0, true, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_if_match",
			Message: "If-Match: * is not supported; pass the resource version",
		}
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, true, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_if_match",
			Message: "If-Match must be a non-negative integer version",
		}
	}
	return n, true, nil
}

// ResolveExpectedVersion picks the expected version for a versioned write.
// Header wins over body. Returns (version, present, requestError).
//
//   - If the header is present and valid, returns (n, true, nil).
//   - If the header is present and malformed, returns (0, true, *RequestError).
//   - If the header is absent and bodyVersion is non-nil, returns (*bodyVersion, true, nil).
//   - If neither is provided, returns (0, false, nil) — caller decides whether
//     to enforce per RequireIfMatch().
func ResolveExpectedVersion(c *gin.Context, bodyVersion *int) (int, bool, error) {
	if v, present, err := ParseIfMatchHeader(c); err != nil {
		return 0, true, err
	} else if present {
		return v, true, nil
	}
	if bodyVersion != nil {
		if *bodyVersion < 0 {
			return 0, true, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_version",
				Message: "version body field must be a non-negative integer",
			}
		}
		return *bodyVersion, true, nil
	}
	return 0, false, nil
}

// EnforceIfMatchOrWarn applies the rollout policy when the caller did not
// supply a version. When RequireIfMatch() is true, returns a 428 RequestError;
// otherwise sets the Deprecation/Warning headers and returns nil.
func EnforceIfMatchOrWarn(c *gin.Context) error {
	if RequireIfMatch() {
		return &RequestError{
			Status:  http.StatusPreconditionRequired,
			Code:    "if_match_required",
			Message: "If-Match header is required for this operation",
		}
	}
	c.Header("Deprecation", VersionDeprecationLink)
	c.Header("Warning", VersionDeprecationMessage)
	return nil
}

// SetETagHeader writes the ETag response header for a versioned entity.
// Per RFC 7232 ETag values are double-quoted opaque tokens; we use the
// integer version as the value.
func SetETagHeader(c *gin.Context, version int) {
	c.Header("ETag", `"`+strconv.Itoa(version)+`"`)
}

// CheckAndBumpVersion atomically validates the caller's expected version and
// increments the row's version by one. Returns the new version on success.
//
// Errors:
//   - dberrors.ErrNotFound  if the row with id does not exist.
//   - ErrVersionMismatch    if the row exists but version != expected.
//   - other GORM errors are returned wrapped via dberrors.Classify.
//
// This is intended to be called BEFORE the entity's content UPDATE inside
// the same transaction. Concurrent writers race on this single UPDATE: the
// first to commit wins and the loser sees rows-affected = 0, which we map
// to ErrVersionMismatch (after a separate existence probe to distinguish
// 404 from 409).
//
// tableName must be the physical DB table name (e.g. "threat_models").
// On Oracle, GORM lowercases the WHERE column references; the column is
// "version" on both PostgreSQL and Oracle (case-insensitive identifier).
func CheckAndBumpVersion(ctx context.Context, db *gorm.DB, tableName, id string, expected int) (int, error) {
	tx := db.WithContext(ctx).Table(tableName).
		Where("id = ? AND version = ?", id, expected).
		UpdateColumn("version", gorm.Expr("version + 1"))
	if tx.Error != nil {
		return 0, dberrors.Classify(tx.Error)
	}
	if tx.RowsAffected == 0 {
		// Distinguish 404 (row missing) from 409 (version mismatch).
		var count int64
		probe := db.WithContext(ctx).Table(tableName).Where("id = ?", id).Count(&count)
		if probe.Error != nil {
			return 0, dberrors.Classify(probe.Error)
		}
		if count == 0 {
			return 0, dberrors.ErrNotFound
		}
		return 0, ErrVersionMismatch
	}
	return expected + 1, nil
}

// MapVersionError converts a store-layer error into the appropriate HTTP
// RequestError for the optimistic-locking contract. Returns nil if the
// error is not a versioning error so callers can fall through to their
// existing error mapping.
func MapVersionError(err error) *RequestError {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrVersionMismatch) {
		return &RequestError{
			Status:  http.StatusConflict,
			Code:    "version_mismatch",
			Message: "Resource version does not match If-Match precondition; refetch and retry",
		}
	}
	return nil
}

// VersionedStore is the minimal interface a Gorm-backed store implements to
// participate in optimistic locking. Each entity store calls into the central
// CheckAndBumpVersion helper; this interface exists primarily to type-assert
// the package-level store globals (which are typed as broader interfaces) at
// the handler boundary without introducing circular references.
type VersionedStore interface {
	CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error)
}

// ApplyOptimisticLock implements the handler-side flow:
//
//  1. Resolve expectedVersion from If-Match header (preferred) or body
//     "version" field (fallback).
//  2. If neither is supplied, defer to RequireIfMatch(): either return a 428
//     RequestError or set Deprecation/Warning headers and return (0, false, nil).
//  3. If a version is supplied, call store.CheckAndBumpVersion. On version
//     mismatch return a 409 RequestError; on missing row return nil so the
//     caller's existing not-found mapping fires.
//
// On success the new version is returned so the handler can stamp the ETag
// response header before serializing the response body.
func ApplyOptimisticLock(c *gin.Context, store VersionedStore, id string, bodyVersion *int) (newVersion int, present bool, err error) {
	expected, hasVersion, parseErr := ResolveExpectedVersion(c, bodyVersion)
	if parseErr != nil {
		return 0, false, parseErr
	}
	if !hasVersion {
		if e := EnforceIfMatchOrWarn(c); e != nil {
			return 0, false, e
		}
		return 0, false, nil
	}
	bumped, casErr := store.CheckAndBumpVersion(c.Request.Context(), id, expected)
	if casErr != nil {
		if mapped := MapVersionError(casErr); mapped != nil {
			return 0, true, mapped
		}
		// Missing row falls through to caller's not-found mapping.
		return 0, true, casErr
	}
	return bumped, true, nil
}

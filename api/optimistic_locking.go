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
//
// Oracle migration shape (#391, verified):
//
//	gorm-oracle v1.1.1 Migrator.AddColumn (oracle/migrator.go:298) emits a
//	SINGLE-statement ALTER TABLE for new columns:
//	  ALTER TABLE <table> ADD (<col> <DataType> DEFAULT 1 NOT NULL)
//	via FullDataTypeOf (oracle/migrator.go:615), which concatenates type +
//	default + NOT NULL in one clause.Expr. Oracle 12c+ / 19c ADB stores the
//	default in SYS.ECOL$ as metadata and synthesizes it for pre-existing
//	rows on read until they are rewritten, so the rollout does NOT take a
//	row-rewrite TM lock on threat_models / diagrams / assets / threats /
//	documents. If the gorm-oracle dependency is bumped, re-verify this
//	emission shape — the two-statement form (ADD + MODIFY NOT NULL) would
//	re-scan every row.
package api

import (
	"context"
	"database/sql"
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
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: store the global If-Match enforcement flag at server startup (mutates shared state)
func SetRequireIfMatch(v bool) {
	requireIfMatchFlag.Store(v)
}

// RequireIfMatch reports whether missing If-Match should hard-fail with 428.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: report whether missing If-Match header should return 428 (pure)
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

// VersionWildcard is the sentinel expected-version value meaning "match any
// current version" — the CAS bump in CheckAndBumpVersion applies
// unconditionally. It is negative so it can never collide with a real
// version (all other call sites enforce n >= 0).
const VersionWildcard = -1

// ParseIfMatchHeader extracts the expected version from the If-Match request
// header. Returns (version, true, nil) on success, (0, false, nil) if the
// header is absent, or (0, true, err) if the header is present but
// malformed.
//
// Per RFC 7232 If-Match values are quoted ETags. We accept either a bare
// integer ("If-Match: 5"), a quoted integer (`If-Match: "5"`), or the "*"
// wildcard, which RFC 7232 defines as matching any current representation —
// we map it to VersionWildcard so the write proceeds unconditionally.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: parse an integer version or wildcard from the If-Match request header (pure)
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
		return VersionWildcard, true, nil
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
//
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: resolve expected resource version from If-Match header or body fallback (pure)
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
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: return 428 when If-Match is required, or set deprecation warning headers when lenient
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
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: write the versioned ETag response header for a resource (pure)
func SetETagHeader(c *gin.Context, version int) {
	c.Header("ETag", `"`+strconv.Itoa(version)+`"`)
}

// CheckAndBumpVersion atomically validates the caller's expected version and
// increments the row's version by one. Returns the new version on success.
//
// If expected is VersionWildcard (the caller sent "If-Match: *", RFC 7232's
// "match any current representation"), the bump is unconditional: the WHERE
// clause carries only the id, and the new version is read back with a
// SELECT since UpdateColumn does not return the updated row's value on
// either PostgreSQL or Oracle. A clause.Returning cannot substitute for the
// read-back here: gorm-oracle only builds `RETURNING ... INTO` when
// stmt.Schema is non-nil, and this call uses Table() with no model, so
// Schema is nil and a Returning clause would be silently ignored (verified
// against gorm-oracle's Dialector.UpdateFromRewrite).
//
// Errors:
//   - dberrors.ErrNotFound  if the row with id does not exist.
//   - ErrVersionMismatch    if the row exists but version != expected. Under
//     VersionWildcard this is also returned (instead of a bare classified
//     error) when the bump committed but the read-back could not confirm the
//     resulting version — see the wildcard branch below for why.
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
// SEM@9fa66a9bf47d32b91bc4119acc795e307691601a: atomically validate expected version (or wildcard) and increment the row version in the DB (reads DB)
func CheckAndBumpVersion(ctx context.Context, db *gorm.DB, tableName, id string, expected int) (int, error) {
	query := db.WithContext(ctx).Table(tableName).Where("id = ?", id)
	if expected != VersionWildcard {
		query = query.Where("version = ?", expected)
	}
	tx := query.UpdateColumn("version", gorm.Expr("version + 1"))
	// gorm-oracle@v1.1.3's checkMissingWhereConditions runs BEFORE
	// executeUpdate (oracle/update.go:126 vs :132) — it is a PRE-EXECUTION
	// build check, so it can never fire alongside a successful row update;
	// tx.RowsAffected == 0 whenever tx.Error carries this message. Its real
	// trigger is isSoftDeleteExprCondition (oracle/update.go:251-257), which
	// discards any clause.Expr whose SQL merely *contains* both
	// "deleted_at" and "null" — e.g. the tombstone cascade's
	// `Where("threat_model_id = ? AND deleted_at IS NULL", id)` (see
	// api/tombstone_store.go, #392). Neither "id = ?" nor "version = ?"
	// contains "deleted_at", so this heuristic cannot trigger for this
	// function's WHERE clauses; the swallow below is dead code here today,
	// kept only so a future change to this query's WHERE shape doesn't
	// silently reintroduce #392's confusing 500.
	if tx.Error != nil && !isOracleSpuriousNoRowsErr(tx.Error) {
		return 0, dberrors.Classify(tx.Error)
	}
	if tx.RowsAffected == 0 {
		if expected == VersionWildcard {
			// No version predicate is in play under the wildcard, so
			// RowsAffected == 0 normally means the row does not exist.
			// Guard (#581 finding 2 hardening): only conclude that when
			// tx.Error is nil. tx.Error can only be non-nil here because
			// isOracleSpuriousNoRowsErr swallowed it above; if a future
			// gorm-oracle change broadens that heuristic to swallow a real
			// error against a *live* row, don't blindly report 404 for it —
			// classify and surface it instead.
			if tx.Error != nil {
				return 0, dberrors.Classify(tx.Error)
			}
			return 0, dberrors.ErrNotFound
		}
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
	if expected == VersionWildcard {
		// The bump above committed (RowsAffected == 1); this SELECT only
		// recovers the resulting value for the ETag. Use a GORM finisher
		// (Pluck) rather than .Row().Scan(): the latter returns a bare
		// *sql.Row whose Scan panics on a nil receiver if GORM's row
		// callback short-circuits, and its only error signal is
		// sql.ErrNoRows, which dberrors.Classify did not previously
		// recognize (#581 finding 1c). Pluck runs the normal "gorm:query"
		// callback chain and exposes both Error and RowsAffected on its
		// returned *gorm.DB. Scanning into sql.NullInt64 (the pattern
		// already used at cmd/server/main.go:1543) also means a NULL
		// version column cannot produce an unclassified "converting NULL to
		// int is unsupported" scan error (#581 finding 1c).
		var newVersion sql.NullInt64
		readTx := db.WithContext(ctx).Table(tableName).Where("id = ?", id).Pluck("version", &newVersion)
		if readTx.Error != nil {
			classified := dberrors.Classify(readTx.Error)
			if errors.Is(classified, dberrors.ErrNotFound) {
				// The row existed a moment ago (the UPDATE above matched
				// it) but is gone now — a hard delete raced the read-back.
				return 0, dberrors.ErrNotFound
			}
			// Any other classified error (transient ADB blip, permission
			// oddity, etc., #581 finding 1b) must not be returned bare:
			// ApplyOptimisticLock only special-cases ErrNotFound and
			// ErrVersionMismatch, so anything else reaches
			// HandleRequestError's else-branch and becomes an
			// undocumented, policy-violating 500 — and unlike a pre-commit
			// failure, the version bump here already committed, so there is
			// nothing left to roll back. We cannot honestly report the
			// resulting version, so treat this the same as a version
			// mismatch: the caller's view of the resource is stale
			// regardless of *why* we can't confirm it, and 409's
			// refetch-and-retry response is already documented for every
			// operation that reaches this path, unlike any 5xx code.
			return 0, ErrVersionMismatch
		}
		if readTx.RowsAffected == 0 || !newVersion.Valid {
			return 0, dberrors.ErrNotFound
		}
		return int(newVersion.Int64), nil
	}
	return expected + 1, nil
}

// MapVersionError converts a store-layer error into the appropriate HTTP
// RequestError for the optimistic-locking contract. Returns nil if the
// error is not a versioning error so callers can fall through to their
// existing error mapping.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: convert a version-mismatch store error to a 409 HTTP request error (pure)
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
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: interface for stores that support atomic version check-and-bump for optimistic locking (pure)
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
// SEM@3256ece0f5730b6c910aa6e61025555c7726a4a5: validate and apply a versioned compare-and-swap lock on a stored resource (reads DB)
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
		// A missing row means the optimistic-lock CAS ran against a resource
		// that does not exist. Map it to a 404 RequestError here rather than
		// returning the bare store error: callers hand this straight to
		// HandleRequestError, which would otherwise classify the unrecognized
		// error as a 500 (violating the Zero-500 policy). (#495 B2)
		if errors.Is(casErr, dberrors.ErrNotFound) {
			return 0, false, NotFoundError("Resource not found")
		}
		return 0, true, casErr
	}
	return bumped, true, nil
}

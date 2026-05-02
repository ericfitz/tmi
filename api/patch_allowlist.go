package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// PatchPathAllowList is the per-resource allowlist of JSON-Pointer prefixes
// a PATCH request may target. Default-deny: any operation whose path is
// not equal to or a child of an allowlisted prefix is rejected with a
// 400 invalid_input.
//
// Replaces the historical "prohibitedPaths" deny-list which was prone to
// silent gaps when new fields were added to a resource (T2/T19/T27 in
// docs/THREAT_MODEL.md).
type PatchPathAllowList struct {
	// MutablePaths is the set of paths a request may freely target.
	// "/foo" matches "/foo" and any deeper path "/foo/...".
	MutablePaths []string

	// SecurityReviewerOnly is the set of paths a request may target ONLY
	// when the caller is a security reviewer or a service account. The
	// matching rule is identical to MutablePaths.
	SecurityReviewerOnly []string

	// OwnerCanClear is the subset of SecurityReviewerOnly paths that the
	// resource owner may also target, but only with a clearing op
	// (an "remove" op, or a "replace" whose value is JSON null). Setting
	// such a path to a non-null value still requires the security
	// reviewer or service account role. This allows an owner to release
	// reviewer-protected fields (e.g. clear /security_reviewer) without
	// granting them the ability to install or change a value.
	OwnerCanClear []string

	// OwnerOnly is the set of paths a request may target ONLY when the
	// caller has owner role on the resource. Same matching rule.
	OwnerOnly []string
}

// PatchAuthContext carries the role bits the allowlist checker needs to
// arbitrate gated paths. All fields default to false.
type PatchAuthContext struct {
	IsOwner            bool
	IsSecurityReviewer bool
	IsServiceAccount   bool
}

// pathMatchesPrefix reports whether path equals prefix or is a child of
// prefix (i.e. prefix == "/foo" matches "/foo", "/foo/bar", "/foo/bar/0",
// but not "/foobar"). Paths use JSON Pointer encoding.
func pathMatchesPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}

// matchesAny reports whether path matches any of the prefixes.
func (l PatchPathAllowList) matchesAny(prefixes []string, path string) bool {
	for _, p := range prefixes {
		if pathMatchesPrefix(path, p) {
			return true
		}
	}
	return false
}

// isClearingOp reports whether op clears its target. A "remove" op always
// clears; a "replace" op clears when its Value is JSON null (Go nil).
func isClearingOp(op PatchOperation) bool {
	switch JsonPatchDocumentOp(op.Op) {
	case Remove:
		return true
	case Replace:
		return op.Value == nil
	default:
		return false
	}
}

// ValidatePatchAllowlist returns an error for any operation whose path is
// not in the allowlist for the given resource type, or that targets a
// gated path without the required role. Returns nil when every operation
// is permitted.
//
// The error wraps a *RequestError with HTTP 400 (path not allowed) or 403
// (path is gated and caller lacks the role). Paths are checked in this
// order: OwnerOnly → SecurityReviewerOnly → MutablePaths → reject. Empty
// path operations and operations whose path lacks a leading "/" are
// rejected as malformed.
func ValidatePatchAllowlist(allow PatchPathAllowList, ops []PatchOperation, ac PatchAuthContext) *RequestError {
	for _, op := range ops {
		if op.Path == "" || op.Path[0] != '/' {
			return InvalidInputError(fmt.Sprintf("Invalid PATCH path: %q", op.Path))
		}

		if allow.matchesAny(allow.OwnerOnly, op.Path) {
			if !ac.IsOwner {
				return ForbiddenError(fmt.Sprintf(
					"Field '%s' may only be modified by the resource owner",
					strings.TrimPrefix(op.Path, "/")))
			}
			continue
		}

		if allow.matchesAny(allow.SecurityReviewerOnly, op.Path) {
			if ac.IsSecurityReviewer || ac.IsServiceAccount {
				continue
			}
			// Owner may clear (but not set) reviewer-gated fields listed
			// in OwnerCanClear. A "remove" op or a "replace" whose value
			// is JSON null both qualify as clearing.
			if ac.IsOwner && allow.matchesAny(allow.OwnerCanClear, op.Path) && isClearingOp(op) {
				continue
			}
			return ForbiddenError(fmt.Sprintf(
				"Field '%s' may only be modified by a security reviewer or a service account",
				strings.TrimPrefix(op.Path, "/")))
		}

		if allow.matchesAny(allow.MutablePaths, op.Path) {
			continue
		}

		return InvalidInputError(fmt.Sprintf(
			"Field '%s' is not allowed in PATCH requests. %s",
			strings.TrimPrefix(op.Path, "/"),
			getFieldErrorMessage(strings.TrimPrefix(op.Path, "/"))))
	}
	return nil
}

// getResourceRoleSafe reads the resource role from the Gin context, returning
// the empty Role if it is unset or malformed. The full GetResourceRole
// helper returns an error in the malformed case; for allowlist purposes,
// "unknown role" must default-deny on owner-gated paths anyway.
func getResourceRoleSafe(c *gin.Context) Role {
	role, err := GetResourceRole(c)
	if err != nil {
		return ""
	}
	return role
}

// getCtxBool reads a boolean key from the Gin context, returning false
// if the key is missing or not a bool.
func getCtxBool(c *gin.Context, key string) bool {
	v, exists := c.Get(key)
	if !exists {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// ThreatModelPatchAllowList is the canonical allowlist for the
// /threat_models/{id} PATCH endpoint. Mirrors the writable fields of
// ThreatModelBase (api/api.go) plus the role-gated workflow and
// authorization fields.
//
// Server-managed fields (id, created_at, modified_at, created_by,
// deleted_at, status_updated) and sub-resource collections (diagrams,
// documents, threats, notes, assets, repositories) are intentionally
// absent and therefore rejected by default.
//
// is_confidential is also intentionally absent: it is set at creation
// and is read-only thereafter (escalating a non-confidential model to
// confidential after the fact would expose data to existing readers).
var ThreatModelPatchAllowList = PatchPathAllowList{
	MutablePaths: []string{
		"/name",
		"/description",
		"/issue_uri",
		"/metadata",
		"/alias",
		"/threat_model_framework",
		"/source_code",
		"/sourceCode",
		"/repository_uri",
		"/project_id",
	},
	SecurityReviewerOnly: []string{
		"/status",
		"/security_reviewer",
	},
	// An owner may clear (but not set) /security_reviewer. This restores
	// the clear-then-remove invariant that lets an owner release a stale
	// reviewer slot without being able to install themselves as reviewer.
	OwnerCanClear: []string{
		"/security_reviewer",
	},
	OwnerOnly: []string{
		"/owner",
		"/authorization",
	},
}

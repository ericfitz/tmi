package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPatchPathAllowList_DefaultDeny pins the most important property:
// any path not on the allowlist is rejected. This is the inverse of the
// historical bug where a deny-list missed /owner, /authorization, /status.
func TestPatchPathAllowList_DefaultDeny(t *testing.T) {
	allow := PatchPathAllowList{
		MutablePaths: []string{"/name", "/description"},
	}

	denied := []string{
		"/id",
		"/created_at",
		"/modified_at",
		"/created_by",
		"/diagrams",
		"/documents",
		"/threats",
		"/sourceCode",
		"/is_confidential",
		"/owner",         // not in allowlist → denied
		"/authorization", // not in allowlist → denied
		"/status",        // not in allowlist → denied
		"/secret_field_we_havent_invented_yet",
	}
	for _, p := range denied {
		ops := []PatchOperation{{Op: "replace", Path: p, Value: "x"}}
		err := ValidatePatchAllowlist(allow, ops, PatchAuthContext{})
		require := assert.Error
		require(t, err, "path %q must be rejected", p)
	}
}

// TestPatchPathAllowList_PrefixMatching pins that "/foo" matches "/foo"
// and "/foo/bar" but NOT "/foobar". Without this, "/idle" would match
// "/id" and the deny-list bug returns.
func TestPatchPathAllowList_PrefixMatching(t *testing.T) {
	allow := PatchPathAllowList{MutablePaths: []string{"/metadata"}}

	cases := []struct {
		path  string
		allow bool
	}{
		{"/metadata", true},
		{"/metadata/0", true},
		{"/metadata/0/key", true},
		{"/metadataa", false}, // not a child, just shares a prefix
		{"/meta", false},
	}
	for _, tc := range cases {
		ops := []PatchOperation{{Op: "replace", Path: tc.path, Value: "x"}}
		err := ValidatePatchAllowlist(allow, ops, PatchAuthContext{})
		if tc.allow {
			assert.Nil(t, err, "path %q should be allowed", tc.path)
		} else {
			assert.NotNil(t, err, "path %q should be rejected", tc.path)
		}
	}
}

// TestPatchPathAllowList_OwnerOnlyGate pins that owner-only paths (e.g.
// /owner, /authorization) are 403 for non-owners and allowed for owners.
func TestPatchPathAllowList_OwnerOnlyGate(t *testing.T) {
	allow := PatchPathAllowList{
		OwnerOnly: []string{"/owner", "/authorization"},
	}

	for _, p := range []string{"/owner", "/authorization", "/authorization/0/role"} {
		ops := []PatchOperation{{Op: "replace", Path: p, Value: "x"}}

		// Non-owner: 403 forbidden
		err := ValidatePatchAllowlist(allow, ops, PatchAuthContext{IsOwner: false})
		require := assert.NotNil
		require(t, err, "non-owner must not patch %q", p)
		assert.Equal(t, 403, err.Status)
		assert.Equal(t, "forbidden", err.Code)

		// Owner: allowed
		err = ValidatePatchAllowlist(allow, ops, PatchAuthContext{IsOwner: true})
		assert.Nil(t, err, "owner should be allowed to patch %q", p)
	}
}

// TestPatchPathAllowList_SecurityReviewerGate pins that /status is 403 for
// non-reviewers and allowed for reviewers and service accounts.
func TestPatchPathAllowList_SecurityReviewerGate(t *testing.T) {
	allow := PatchPathAllowList{
		SecurityReviewerOnly: []string{"/status"},
	}
	ops := []PatchOperation{{Op: "replace", Path: "/status", Value: "approved"}}

	// Plain writer: 403
	err := ValidatePatchAllowlist(allow, ops, PatchAuthContext{})
	assert.NotNil(t, err)
	assert.Equal(t, 403, err.Status)

	// Security reviewer: allowed
	err = ValidatePatchAllowlist(allow, ops, PatchAuthContext{IsSecurityReviewer: true})
	assert.Nil(t, err)

	// Service account: allowed
	err = ValidatePatchAllowlist(allow, ops, PatchAuthContext{IsServiceAccount: true})
	assert.Nil(t, err)
}

// TestPatchPathAllowList_RejectsMalformedPath confirms an empty or
// unanchored path is rejected, not silently allowed.
func TestPatchPathAllowList_RejectsMalformedPath(t *testing.T) {
	allow := PatchPathAllowList{MutablePaths: []string{"/name"}}

	for _, p := range []string{"", "name", "no-leading-slash"} {
		ops := []PatchOperation{{Op: "replace", Path: p, Value: "x"}}
		err := ValidatePatchAllowlist(allow, ops, PatchAuthContext{})
		assert.NotNil(t, err, "malformed path %q must be rejected", p)
	}
}

// TestThreatModelPatchAllowList_BlocksLegacyHoles regresses the specific
// gaps T2/T19/T27 called out: /owner, /authorization, /status, /id.
//
// Previously /owner and /authorization were enforced only by
// ValidatePatchAuthorization (owner role) and /status had no gate at all.
// The allowlist now closes the writer-can-mutate-status hole and adds a
// belt-and-suspenders check on owner-only fields.
func TestThreatModelPatchAllowList_BlocksLegacyHoles(t *testing.T) {
	allow := ThreatModelPatchAllowList

	// /id: not in allowlist → 400 from any caller.
	idOps := []PatchOperation{{Op: "replace", Path: "/id", Value: "x"}}
	err := ValidatePatchAllowlist(allow, idOps, PatchAuthContext{IsOwner: true, IsSecurityReviewer: true})
	assert.NotNil(t, err)
	assert.Equal(t, 400, err.Status)

	// /status: only allowed for security reviewer / service account.
	statusOps := []PatchOperation{{Op: "replace", Path: "/status", Value: "approved"}}
	err = ValidatePatchAllowlist(allow, statusOps, PatchAuthContext{IsOwner: true})
	assert.NotNil(t, err)
	assert.Equal(t, 403, err.Status, "owner-without-reviewer must NOT mutate /status")

	// /owner and /authorization: only allowed for resource owner.
	for _, p := range []string{"/owner", "/authorization", "/authorization/0/role"} {
		ops := []PatchOperation{{Op: "replace", Path: p, Value: "x"}}
		err = ValidatePatchAllowlist(allow, ops, PatchAuthContext{IsSecurityReviewer: true})
		assert.NotNil(t, err, "non-owner must not patch %q", p)
		assert.Equal(t, 403, err.Status)
	}

	// is_confidential is intentionally absent — escalation must be blocked.
	confOps := []PatchOperation{{Op: "replace", Path: "/is_confidential", Value: true}}
	err = ValidatePatchAllowlist(allow, confOps, PatchAuthContext{IsOwner: true})
	assert.NotNil(t, err)
	assert.Equal(t, 400, err.Status)

	// Sub-resources stay forbidden.
	for _, p := range []string{"/diagrams", "/documents", "/threats", "/notes", "/assets", "/repositories"} {
		ops := []PatchOperation{{Op: "replace", Path: p, Value: "x"}}
		err = ValidatePatchAllowlist(allow, ops, PatchAuthContext{IsOwner: true})
		assert.NotNil(t, err, "sub-resource %q must not be patchable", p)
	}
}

// TestThreatModelPatchAllowList_AllowsCanonicalFields confirms ordinary
// PATCH operations against documented mutable fields succeed.
func TestThreatModelPatchAllowList_AllowsCanonicalFields(t *testing.T) {
	allow := ThreatModelPatchAllowList
	ac := PatchAuthContext{IsOwner: false}

	ok := []string{
		"/name",
		"/description",
		"/issue_uri",
		"/repository_uri",
		"/metadata",
		"/metadata/0/key",
		"/alias",
		"/threat_model_framework",
		"/source_code",
		"/sourceCode",
		"/project_id",
	}
	for _, p := range ok {
		ops := []PatchOperation{{Op: "replace", Path: p, Value: "x"}}
		err := ValidatePatchAllowlist(allow, ops, ac)
		assert.Nil(t, err, "path %q should be allowed for plain writer", p)
	}
}

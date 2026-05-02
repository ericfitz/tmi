package api

import (
	"strings"
	"testing"
)

// fakeSpecJSON is a minimal OpenAPI snippet exercising:
// - public operation with public:true
// - admin operation with roles:[admin]
// - parameterized path
// - operation with no x-tmi-authz (legacy / not yet annotated)
const fakeSpecJSON = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/health": {
      "get": {
        "operationId": "health",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "public": true}
      }
    },
    "/admin/users": {
      "get": {
        "operationId": "listUsers",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
      }
    },
    "/admin/users/{id}": {
      "get": {
        "operationId": "getUser",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
      },
      "delete": {
        "operationId": "deleteUser",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["admin"]}
      }
    },
    "/legacy/path": {
      "get": {
        "operationId": "legacy",
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`

func loadTestTable(t *testing.T) *AuthzTable {
	t.Helper()
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecJSON))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}
	return tbl
}

func TestAuthzTable_LookupExactPath(t *testing.T) {
	tbl := loadTestTable(t)
	rule, ok := tbl.Lookup("GET", "/admin/users")
	if !ok {
		t.Fatal("expected rule for GET /admin/users, got none")
	}
	if rule.Ownership != OwnershipNone {
		t.Errorf("ownership: got %q, want %q", rule.Ownership, OwnershipNone)
	}
	if len(rule.Roles) != 1 || rule.Roles[0] != RoleAuthzAdmin {
		t.Errorf("roles: got %v, want [admin]", rule.Roles)
	}
	if rule.Public {
		t.Errorf("public: got true, want false")
	}
}

func TestAuthzTable_LookupParameterizedPath(t *testing.T) {
	tbl := loadTestTable(t)
	rule, ok := tbl.Lookup("DELETE", "/admin/users/abc-123")
	if !ok {
		t.Fatal("expected rule for DELETE /admin/users/abc-123, got none")
	}
	if rule.Ownership != OwnershipNone || len(rule.Roles) != 1 {
		t.Errorf("rule mismatch: %+v", rule)
	}
}

func TestAuthzTable_PublicOperation(t *testing.T) {
	tbl := loadTestTable(t)
	rule, ok := tbl.Lookup("GET", "/health")
	if !ok {
		t.Fatal("expected rule for GET /health, got none")
	}
	if !rule.Public {
		t.Errorf("public: got false, want true")
	}
	if rule.Ownership != OwnershipNone {
		t.Errorf("ownership: got %q, want %q", rule.Ownership, OwnershipNone)
	}
}

func TestAuthzTable_LookupMissingMethod(t *testing.T) {
	tbl := loadTestTable(t)
	if _, ok := tbl.Lookup("PUT", "/admin/users"); ok {
		t.Error("expected no rule for PUT /admin/users")
	}
}

func TestAuthzTable_LookupUnannotatedPath(t *testing.T) {
	// Legacy path with no x-tmi-authz — Lookup must return ok=false.
	// AuthzMiddleware uses ok=false to mean "pass through to legacy middleware".
	tbl := loadTestTable(t)
	if _, ok := tbl.Lookup("GET", "/legacy/path"); ok {
		t.Error("expected no rule for unannotated /legacy/path")
	}
}

func TestAuthzTable_LookupUnknownPath(t *testing.T) {
	tbl := loadTestTable(t)
	if _, ok := tbl.Lookup("GET", "/does/not/exist"); ok {
		t.Error("expected no rule for unknown path")
	}
}

func TestAuthzTable_RejectsInvalidOwnership(t *testing.T) {
	bad := strings.Replace(fakeSpecJSON, `"ownership": "none", "public": true`, `"ownership": "BOGUS"`, 1)
	if _, err := loadAuthzTableFromJSON([]byte(bad)); err == nil {
		t.Fatal("expected error for invalid ownership value, got nil")
	}
}

func TestAuthzTable_RejectsPublicWithRoles(t *testing.T) {
	// Substitution targets the /health endpoint's verbatim x-tmi-authz block
	// in fakeSpecJSON (the only such block). If the fake spec changes, this
	// test will silently no-op and then fail loudly with err == nil.
	bad := strings.Replace(
		fakeSpecJSON,
		`"x-tmi-authz": {"ownership": "none", "public": true}`,
		`"x-tmi-authz": {"ownership": "none", "public": true, "roles": ["admin"]}`,
		1,
	)
	if _, err := loadAuthzTableFromJSON([]byte(bad)); err == nil {
		t.Fatal("expected error for public+roles combination, got nil")
	}
}

// Multi-template spec for tiebreaker tests: when /threats/bulk and
// /threats/{id} both match a request, the more-literal template wins.
const fakeSpecTiebreaker = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/threats/{id}": {
      "get": {
        "operationId": "getThreat",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      }
    },
    "/threats/bulk": {
      "get": {
        "operationId": "bulkThreats",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    }
  }
}`

func TestAuthzTable_LookupTiebreakerMostLiteralWins(t *testing.T) {
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecTiebreaker))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	// /threats/bulk should match the literal /threats/bulk template, not
	// the parameterized /threats/{id} template, even though the latter
	// also structurally matches.
	rule, ok := tbl.Lookup("GET", "/threats/bulk")
	if !ok {
		t.Fatal("expected rule for GET /threats/bulk, got none")
	}
	if rule.Ownership != OwnershipWriter {
		t.Errorf("most-literal-wins: got ownership=%q (matched param template), want %q (matched literal template)",
			rule.Ownership, OwnershipWriter)
	}

	// A non-bulk id should fall through to the parameterized template.
	rule, ok = tbl.Lookup("GET", "/threats/abc-123")
	if !ok {
		t.Fatal("expected rule for GET /threats/abc-123, got none")
	}
	if rule.Ownership != OwnershipReader {
		t.Errorf("param fallback: got ownership=%q, want %q", rule.Ownership, OwnershipReader)
	}
}

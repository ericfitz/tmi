package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestAdminRoutesDeclareAdminRole pins the #399 security invariant: every
// /admin/* operation in the production OpenAPI spec must (a) carry an
// x-tmi-authz extension that parses into the authz table, and (b) declare the
// "admin" role in that extension. The admin role routes through
// RequireAdministrator, which categorically denies service-account
// (client-credentials) tokens.
//
// Condition (a) matters as much as (b): buildAuthzTable silently skips
// operations with no x-tmi-authz extension, and at runtime AuthzMiddleware
// falls through to legacy (non-admin-gated) middleware for routes absent from
// the table. So this test iterates the spec's paths directly — not the authz
// table — to ensure an /admin operation that omits the extension entirely is
// reported rather than invisibly skipped.
func TestAdminRoutesDeclareAdminRole(t *testing.T) {
	swagger, err := GetSwagger()
	if err != nil {
		t.Fatalf("load embedded OpenAPI spec: %v", err)
	}

	tbl, err := buildAuthzTable(swagger)
	if err != nil {
		t.Fatalf("build authz table: %v", err)
	}

	findings, checked := adminAuthzFindings(swagger, tbl)
	for _, f := range findings {
		t.Error(f)
	}

	// Sanity: the spec has dozens of admin operations; zero means the spec
	// walk or the prefix match broke, not that the invariant holds.
	if checked < 30 {
		t.Fatalf("only %d /admin operations found in the spec; expected 60+ — the invariant check is not seeing the real spec", checked)
	}
}

// TestAdminRoutesDeclareAdminRole_DetectsUnannotatedOperation proves the
// invariant check has no blind spot: it strips the x-tmi-authz extension from
// one /admin operation in an independent copy of the embedded spec and
// asserts that adminAuthzFindings reports it. Without this, a regression that
// made the check iterate only the authz table (which silently omits
// unannotated operations) would pass unnoticed.
func TestAdminRoutesDeclareAdminRole_DetectsUnannotatedOperation(t *testing.T) {
	// GetSwagger re-parses the embedded spec bytes on every call, so this
	// copy is independent and safe to mutate.
	swagger, err := GetSwagger()
	if err != nil {
		t.Fatalf("load embedded OpenAPI spec: %v", err)
	}

	// Strip x-tmi-authz from the first /admin operation we find.
	var strippedMethod, strippedPath string
	for path, item := range swagger.Paths.Map() {
		if !strings.HasPrefix(path, "/admin") {
			continue
		}
		for method, op := range item.Operations() {
			if op == nil {
				continue
			}
			if _, ok := op.Extensions["x-tmi-authz"]; ok {
				delete(op.Extensions, "x-tmi-authz")
				strippedMethod, strippedPath = method, path
				break
			}
		}
		if strippedPath != "" {
			break
		}
	}
	if strippedPath == "" {
		t.Fatal("no /admin operation with x-tmi-authz found to strip; spec walk is broken")
	}

	tbl, err := buildAuthzTable(swagger)
	if err != nil {
		t.Fatalf("build authz table from mutated spec: %v", err)
	}

	findings, _ := adminAuthzFindings(swagger, tbl)
	for _, f := range findings {
		if strings.Contains(f, strippedMethod) && strings.Contains(f, strippedPath) &&
			strings.Contains(f, "no x-tmi-authz") {
			return // the blind spot is covered
		}
	}
	t.Fatalf("stripped x-tmi-authz from %s %s but adminAuthzFindings did not report it; findings: %v",
		strippedMethod, strippedPath, findings)
}

// TestAdminAuthzFindings_DetectsRuleWithoutAdminRole covers the second
// failure mode: an /admin operation that HAS an x-tmi-authz extension but
// omits the admin role from it.
func TestAdminAuthzFindings_DetectsRuleWithoutAdminRole(t *testing.T) {
	const spec = `{
		"openapi": "3.0.3",
		"info": {"title": "t", "version": "1"},
		"paths": {
			"/admin/widgets": {
				"get": {
					"operationId": "listWidgets",
					"x-tmi-authz": {"ownership": "none", "roles": []},
					"responses": {"200": {"description": "ok"}}
				}
			}
		}
	}`
	loader := openapi3.NewLoader()
	swagger, err := loader.LoadFromData([]byte(spec))
	if err != nil {
		t.Fatalf("load test spec: %v", err)
	}
	tbl, err := buildAuthzTable(swagger)
	if err != nil {
		t.Fatalf("build authz table: %v", err)
	}
	findings, checked := adminAuthzFindings(swagger, tbl)
	if checked != 1 {
		t.Fatalf("checked = %d, want 1", checked)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "does not declare the admin role") {
		t.Fatalf("expected one missing-admin-role finding, got: %v", findings)
	}
}

// adminAuthzFindings walks every operation under /admin in the spec and
// returns one finding per violation of the #399 invariant, plus the number of
// /admin operations examined. A violation is either a missing authz-table
// rule (no x-tmi-authz extension — at runtime AuthzMiddleware would fall
// through to legacy middleware and never invoke the admin gate) or a rule
// that does not include RoleAuthzAdmin.
func adminAuthzFindings(swagger *openapi3.T, tbl *AuthzTable) (findings []string, checked int) {
	for path, item := range swagger.Paths.Map() {
		if !strings.HasPrefix(path, "/admin") {
			continue
		}
		for method, op := range item.Operations() {
			if op == nil {
				continue
			}
			checked++
			rule, ok := tbl.byMethodPath[method][path]
			if !ok {
				findings = append(findings, fmt.Sprintf(
					"%s %s has no x-tmi-authz rule in the authz table (extension missing "+
						"or not parsed); at runtime AuthzMiddleware falls through to legacy "+
						"middleware for this route and service-account tokens are NOT "+
						"categorically denied (see #399)", method, path))
				continue
			}
			hasAdmin := false
			for _, r := range rule.Roles {
				if r == RoleAuthzAdmin {
					hasAdmin = true
					break
				}
			}
			if !hasAdmin {
				findings = append(findings, fmt.Sprintf(
					"%s %s does not declare the admin role in x-tmi-authz; every /admin/* "+
						"operation must require it so service-account tokens are denied "+
						"(see #399); rule: %s", method, path, mustJSON(rule)))
			}
		}
	}
	return findings, checked
}

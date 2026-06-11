package api

import (
	"strings"
	"testing"
)

// TestAdminRoutesDeclareAdminRole pins the #399 security invariant: every
// /admin/* operation in the production OpenAPI spec must declare the "admin"
// role in x-tmi-authz. The admin role routes through RequireAdministrator,
// which categorically denies service-account (client-credentials) tokens —
// an /admin operation without the role would silently bypass that denial.
func TestAdminRoutesDeclareAdminRole(t *testing.T) {
	swagger, err := GetSwagger()
	if err != nil {
		t.Fatalf("load embedded OpenAPI spec: %v", err)
	}

	tbl, err := buildAuthzTable(swagger)
	if err != nil {
		t.Fatalf("build authz table: %v", err)
	}

	checked := 0
	for method, byPath := range tbl.byMethodPath {
		for path, rule := range byPath {
			if !strings.HasPrefix(path, "/admin") {
				continue
			}
			checked++
			hasAdmin := false
			for _, r := range rule.Roles {
				if r == RoleAuthzAdmin {
					hasAdmin = true
					break
				}
			}
			if !hasAdmin {
				t.Errorf("%s %s does not declare the admin role in x-tmi-authz; "+
					"every /admin/* operation must require it so service-account "+
					"tokens are denied (see #399)", method, path)
			}
		}
	}

	// Sanity: the spec has dozens of admin operations; zero means the table
	// or the prefix match broke, not that the invariant holds.
	if checked < 30 {
		t.Fatalf("only %d /admin operations found in the authz table; expected 60+ — the invariant check is not seeing the real spec", checked)
	}
}

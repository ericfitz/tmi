package api

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestStepUpRouteTable_IdentityLinkRoutesPresent is the load-bearing spec-pin:
// the three identity-link step-up routes declared with x-tmi-authz-step-up: required
// in the embedded production spec MUST remain present and required after any spec edit.
// This test fails at build time if a future spec change accidentally drops the extension,
// making the link flow silently bypassable.
func TestStepUpRouteTable_IdentityLinkRoutesPresent(t *testing.T) {
	swagger, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger: %v", err)
	}
	table := BuildStepUpRouteTable(swagger)

	required := []struct {
		method string
		path   string
	}{
		{"POST", "/me/identities/link/start"},
		{"POST", "/me/identities/link/confirm"},
		{"DELETE", "/me/identities/{id}"},
	}
	for _, r := range required {
		if !table.Required(r.method, r.path) {
			t.Errorf(
				"%s %s must be step-up-required in the OpenAPI spec "+
					"(x-tmi-authz-step-up: required); a spec edit removed it — restore the extension",
				r.method, r.path)
		}
	}
}

func TestBuildStepUpRouteTable_DefaultsToRequiredForAdminWrites(t *testing.T) {
	spec := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}
	put := &openapi3.Operation{}
	pathItem := &openapi3.PathItem{Put: put}
	spec.Paths.Set("/admin/settings/{key}", pathItem)

	table := BuildStepUpRouteTable(spec)
	if !table.Required("PUT", "/admin/settings/{key}") {
		t.Errorf("PUT /admin/settings/{key} should be required by default")
	}
}

func TestBuildStepUpRouteTable_HonorsOptionalOptOut(t *testing.T) {
	spec := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}
	put := &openapi3.Operation{
		Extensions: map[string]any{
			"x-tmi-authz-step-up": "optional",
		},
	}
	pathItem := &openapi3.PathItem{Put: put}
	spec.Paths.Set("/admin/surveys/{id}", pathItem)

	table := BuildStepUpRouteTable(spec)
	if table.Required("PUT", "/admin/surveys/{id}") {
		t.Errorf("PUT /admin/surveys/{id} marked optional should not require step-up")
	}
}

func TestBuildStepUpRouteTable_IgnoresGETAndNonAdmin(t *testing.T) {
	spec := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}
	get := &openapi3.Operation{}
	put := &openapi3.Operation{}
	spec.Paths.Set("/admin/settings/{key}", &openapi3.PathItem{Get: get})
	spec.Paths.Set("/threat_models/{id}", &openapi3.PathItem{Put: put})

	table := BuildStepUpRouteTable(spec)
	if table.Required("GET", "/admin/settings/{key}") {
		t.Errorf("GET should never require step-up")
	}
	if table.Required("PUT", "/threat_models/{id}") {
		t.Errorf("Non-/admin/* routes should never require step-up")
	}
}

func TestBuildStepUpRouteTable_AllWriteMethods(t *testing.T) {
	spec := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}
	pathItem := &openapi3.PathItem{
		Post:   &openapi3.Operation{},
		Put:    &openapi3.Operation{},
		Patch:  &openapi3.Operation{},
		Delete: &openapi3.Operation{},
	}
	spec.Paths.Set("/admin/foo", pathItem)
	table := BuildStepUpRouteTable(spec)

	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if !table.Required(m, "/admin/foo") {
			t.Errorf("%s /admin/foo should be required by default", m)
		}
	}
}

func TestBuildStepUpRouteTable_EmptySpec(t *testing.T) {
	// nil spec should not panic.
	table := BuildStepUpRouteTable(nil)
	if table.Required("PUT", "/admin/anything") {
		t.Errorf("empty/nil spec should produce empty table")
	}
}

func TestBuildStepUpRouteTable_NonAdminWithRequiredExtension(t *testing.T) {
	spec := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}
	// POST on /me/identities/link/start with x-tmi-authz-step-up: required
	post := &openapi3.Operation{
		Extensions: map[string]any{
			"x-tmi-authz-step-up": "required",
		},
	}
	// POST on /me/identities without the extension — must NOT be in table
	postNoExt := &openapi3.Operation{}
	spec.Paths.Set("/me/identities/link/start", &openapi3.PathItem{Post: post})
	spec.Paths.Set("/me/identities", &openapi3.PathItem{Post: postNoExt})

	table := BuildStepUpRouteTable(spec)
	if !table.Required("POST", "/me/identities/link/start") {
		t.Errorf("POST /me/identities/link/start with x-tmi-authz-step-up=required should be in table")
	}
	if table.Required("POST", "/me/identities") {
		t.Errorf("POST /me/identities without extension should NOT be in table")
	}
}

func TestBuildStepUpRouteTable_RequiredExtensionAlsoWorksOnAdminRoute(t *testing.T) {
	spec := &openapi3.T{
		Paths: openapi3.NewPaths(),
	}
	// GET on /admin/something with required extension (GET is not a default write method)
	get := &openapi3.Operation{
		Extensions: map[string]any{
			"x-tmi-authz-step-up": "required",
		},
	}
	spec.Paths.Set("/admin/special", &openapi3.PathItem{Get: get})

	table := BuildStepUpRouteTable(spec)
	if !table.Required("GET", "/admin/special") {
		t.Errorf("GET /admin/special with required extension should be in table")
	}
}

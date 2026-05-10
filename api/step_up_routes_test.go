package api

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

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

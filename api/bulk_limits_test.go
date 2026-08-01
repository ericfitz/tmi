package api

import "testing"

// TestMaxBulkMetadataFor_KnownEntities pins the two-tier metadata cap. The
// threat-model subtree caps at 20 and the resources that own their metadata cap
// at 100; both values mirror `maxItems` in api-schema/tmi-openapi.json.
func TestMaxBulkMetadataFor_KnownEntities(t *testing.T) {
	cases := map[string]int{
		"threat_model":    20,
		"threat":          20,
		"document":        20,
		"repository":      20,
		"diagram":         20,
		"note":            100,
		"asset":           100,
		"survey":          100,
		"survey_response": 100,
		"team":            100,
		"project":         100,
	}

	for entityType, want := range cases {
		if got := MaxBulkMetadataFor(entityType); got != want {
			t.Errorf("MaxBulkMetadataFor(%q) = %d, want %d", entityType, got, want)
		}
	}
}

// TestMaxBulkMetadataFor_UnknownEntityFailsClosed guards the fallback: a new
// metadata handler that forgets to register itself must end up stricter than
// the spec, never unbounded.
func TestMaxBulkMetadataFor_UnknownEntityFailsClosed(t *testing.T) {
	got := MaxBulkMetadataFor("entity_type_that_does_not_exist")
	if got != maxBulkMetadataSubResource {
		t.Errorf("MaxBulkMetadataFor(unknown) = %d, want the restrictive default %d",
			got, maxBulkMetadataSubResource)
	}
	if got > maxBulkMetadataResource {
		t.Errorf("fallback %d is more permissive than the highest spec cap %d",
			got, maxBulkMetadataResource)
	}
}

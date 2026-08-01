package api

// Bulk-operation size limits.
//
// Every value here mirrors a `maxItems` declared on the corresponding request
// body (or query parameter) in api-schema/tmi-openapi.json. The spec is the
// source of truth; these constants exist only so a handler that is reached
// without the OpenAPI validation middleware still refuses an oversized batch.
//
// Keeping the two in step matters in both directions. A handler limit that is
// *looser* than the spec is dead code, because the middleware rejects the
// request first. A handler limit that is *stricter* silently rejects requests
// the published contract says are valid -- which is what the flat metadata cap
// of 20 was doing to the six resources whose spec allows 100.
//
// If you change a `maxItems` in the spec, change the matching constant here.
const (
	// MaxBulkThreats caps POST/PUT /threat_models/{id}/threats/bulk.
	MaxBulkThreats = 20
	// MaxBulkThreatPatches caps the `patches` array of PATCH
	// /threat_models/{id}/threats/bulk (schema BulkPatchRequest).
	MaxBulkThreatPatches = 20
	// MaxBulkThreatDeletes caps the `threat_ids` query parameter of DELETE
	// /threat_models/{id}/threats/bulk (parameter ThreatIdsQueryParam).
	MaxBulkThreatDeletes = 20
	// MaxBulkDocuments caps POST/PUT /threat_models/{id}/documents/bulk.
	MaxBulkDocuments = 20
	// MaxBulkRepositories caps POST/PUT /threat_models/{id}/repositories/bulk.
	MaxBulkRepositories = 20
	// MaxBulkAssets caps POST/PUT /threat_models/{id}/assets/bulk.
	MaxBulkAssets = 50
)

// Metadata bulk caps. The spec deliberately uses two different values: the
// threat-model subtree caps at 20, while the resources that own their metadata
// outright cap at 100 (matching the `maxItems: 100` every entity schema
// declares on its own `metadata` array).
const (
	maxBulkMetadataSubResource = 20
	maxBulkMetadataResource    = 100
)

// maxBulkMetadataByEntity maps a GenericMetadataHandler's entity type to the
// `maxItems` its .../metadata/bulk operations declare in the spec. The keys are
// exactly the entity-type strings passed to NewGenericMetadataHandler in
// server.go.
var maxBulkMetadataByEntity = map[string]int{
	"threat_model":    maxBulkMetadataSubResource,
	"threat":          maxBulkMetadataSubResource,
	"document":        maxBulkMetadataSubResource,
	"repository":      maxBulkMetadataSubResource,
	"diagram":         maxBulkMetadataSubResource,
	"note":            maxBulkMetadataResource,
	"asset":           maxBulkMetadataResource,
	"survey":          maxBulkMetadataResource,
	"survey_response": maxBulkMetadataResource,
	"team":            maxBulkMetadataResource,
	"project":         maxBulkMetadataResource,
}

// MaxBulkMetadataFor returns the bulk metadata cap for an entity type. An
// unregistered entity type falls back to the more restrictive value rather than
// the more permissive one, so a new handler that forgets to add itself here is
// merely stricter than the spec instead of unbounded.
// SEM@new: return the spec-declared bulk metadata cap for an entity type (pure)
func MaxBulkMetadataFor(entityType string) int {
	if limit, ok := maxBulkMetadataByEntity[entityType]; ok {
		return limit
	}
	return maxBulkMetadataSubResource
}

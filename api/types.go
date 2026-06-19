package api

import (
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// TypesUUID is an alias for openapi_types.UUID to make it easier to use
type TypesUUID = openapi_types.UUID

// ParseUUID converts a string to a TypesUUID
// SEM@b68e94b8e6dacc1b4643b872626ca878a7789b60: parse a UUID string and return a typed UUID or an error (pure)
func ParseUUID(s string) (TypesUUID, error) {
	// Parse the UUID from string
	parsedUUID, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, err
	}
	return parsedUUID, nil
}

// NewUUID generates a new UUID
// SEM@386eea01f3b66c35027bf3ca762efbc291419e20: generate a new random UUID (pure)
func NewUUID() TypesUUID {
	return uuid.New()
}

// CurrentTime returns current time in UTC
// SEM@386eea01f3b66c35027bf3ca762efbc291419e20: return the current time in UTC (pure)
func CurrentTime() time.Time {
	return time.Now().UTC()
}

// DiagramRequest is used for creating and updating diagrams
// SEM@15f518ec5394b0508ff8c84d08d6c785286d76e2: request DTO for creating or updating a diagram (pure)
type DiagramRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description,omitempty"`
	GraphData   []Cell  `json:"graphData,omitempty"`
}

// Component represents a diagram component
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: represent a typed diagram component with associated metadata (pure)
type Component struct {
	ID       string         `json:"id"`
	Type     string         `json:"type" binding:"required"`
	Data     map[string]any `json:"data"`
	Metadata []MetadataItem `json:"metadata,omitempty"`
}

// MetadataItem represents a metadata key-value pair
// SEM@386eea01f3b66c35027bf3ca762efbc291419e20: represent a single metadata key-value pair (pure)
type MetadataItem struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value" binding:"required"`
}

// ThreatModelRequest is used for creating and updating threat models
// SEM@386eea01f3b66c35027bf3ca762efbc291419e20: request DTO for creating or updating a threat model (pure)
type ThreatModelRequest struct {
	Name        string         `json:"name" binding:"required"`
	Description *string        `json:"description,omitempty"`
	DiagramIDs  []string       `json:"diagram_ids,omitempty"`
	Threats     []ThreatEntity `json:"threats,omitempty"`
}

// ThreatEntity represents a threat in a threat model (custom name to avoid collision with generated Threat)
// SEM@386eea01f3b66c35027bf3ca762efbc291419e20: represent a threat within a threat model, avoiding collision with the generated Threat type (pure)
type ThreatEntity struct {
	ID          string         `json:"id,omitempty"`
	Name        string         `json:"name" binding:"required"`
	Description *string        `json:"description,omitempty"`
	Metadata    []MetadataItem `json:"metadata,omitempty"`
}

// AuthUser represents authenticated user information
// SEM@386eea01f3b66c35027bf3ca762efbc291419e20: represent an authenticated user's identity and access token (pure)
type AuthUser struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SetCreatedAt implements WithTimestamps interface for DfdDiagram
// SEM@de36efe7c9abdb238cea29efae95937751a21874: set the created_at timestamp on a DfdDiagram to implement WithTimestamps (pure)
func (d *DfdDiagram) SetCreatedAt(t time.Time) {
	d.CreatedAt = &t
}

// SetModifiedAt implements WithTimestamps interface for DfdDiagram
// SEM@de36efe7c9abdb238cea29efae95937751a21874: set the modified_at timestamp on a DfdDiagram to implement WithTimestamps (pure)
func (d *DfdDiagram) SetModifiedAt(t time.Time) {
	d.ModifiedAt = &t
}

// SetCreatedAt implements WithTimestamps interface
// SEM@de36efe7c9abdb238cea29efae95937751a21874: set the created_at timestamp on a ThreatModel to implement WithTimestamps (pure)
func (t *ThreatModel) SetCreatedAt(time time.Time) {
	t.CreatedAt = &time
}

// SetModifiedAt implements WithTimestamps interface
// SEM@de36efe7c9abdb238cea29efae95937751a21874: set the modified_at timestamp on a ThreatModel to implement WithTimestamps (pure)
func (t *ThreatModel) SetModifiedAt(time time.Time) {
	t.ModifiedAt = &time
}

// PatchOperation represents a JSON Patch operation
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: represent a single JSON Patch operation with op, path, value, and from fields (pure)
type PatchOperation struct {
	Op    string `json:"op" binding:"required,oneof=add remove replace move copy test"`
	Path  string `json:"path" binding:"required"`
	Value any    `json:"value,omitempty"`
	From  string `json:"from,omitempty"`
}

// ValidationError represents a validation error
// SEM@386eea01f3b66c35027bf3ca762efbc291419e20: represent a field-level validation failure with field name and message (pure)
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ErrorResponse is deprecated. Use the OpenAPI-generated Error type instead.
// This type has been replaced with api.Error which uses error_description field
// per OpenAPI specification requirements.
//
// Deprecated: Use Error from api.go (OpenAPI-generated)
type ErrorResponse = Error

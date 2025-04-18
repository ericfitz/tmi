package api

import (
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// TypesUUID is an alias for openapi_types.UUID to make it easier to use
type TypesUUID = openapi_types.UUID

// ParseUUID converts a string to a TypesUUID
func ParseUUID(s string) (TypesUUID, error) {
	// Parse the UUID from string
	parsedUUID, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, err
	}
	return parsedUUID, nil
}

// NewUUID generates a new UUID
func NewUUID() TypesUUID {
	return uuid.New()
}

// CurrentTime returns current time in UTC
func CurrentTime() time.Time {
	return time.Now().UTC()
}

// DiagramRequest is used for creating and updating diagrams
type DiagramRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description,omitempty"`
	GraphData   []Cell  `json:"graphData,omitempty"`
}

// Component represents a diagram component
type Component struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type" binding:"required"`
	Data     map[string]interface{} `json:"data"`
	Metadata []MetadataItem         `json:"metadata,omitempty"`
}

// MetadataItem represents a metadata key-value pair
type MetadataItem struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value" binding:"required"`
}

// ThreatModelRequest is used for creating and updating threat models
type ThreatModelRequest struct {
	Name        string         `json:"name" binding:"required"`
	Description *string        `json:"description,omitempty"`
	DiagramIDs  []string       `json:"diagram_ids,omitempty"`
	Threats     []ThreatEntity `json:"threats,omitempty"`
}

// ThreatEntity represents a threat in a threat model (custom name to avoid collision with generated Threat)
type ThreatEntity struct {
	ID          string         `json:"id,omitempty"`
	Name        string         `json:"name" binding:"required"`
	Description *string        `json:"description,omitempty"`
	Metadata    []MetadataItem `json:"metadata,omitempty"`
}

// AuthUser represents authenticated user information
type AuthUser struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SetCreatedAt implements WithTimestamps interface
func (d *Diagram) SetCreatedAt(t time.Time) {
	d.CreatedAt = t
}

// SetModifiedAt implements WithTimestamps interface
func (d *Diagram) SetModifiedAt(t time.Time) {
	d.ModifiedAt = t
}

// SetCreatedAt implements WithTimestamps interface
func (t *ThreatModel) SetCreatedAt(time time.Time) {
	t.CreatedAt = time
}

// SetModifiedAt implements WithTimestamps interface
func (t *ThreatModel) SetModifiedAt(time time.Time) {
	t.ModifiedAt = time
}

// PatchOperation represents a JSON Patch operation
type PatchOperation struct {
	Op    string      `json:"op" binding:"required,oneof=add remove replace move copy test"`
	Path  string      `json:"path" binding:"required"`
	Value interface{} `json:"value,omitempty"`
	From  string      `json:"from,omitempty"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ErrorResponse is a standardized error response
type ErrorResponse struct {
	Error       string            `json:"error"`
	Message     string            `json:"message"`
	Validations []ValidationError `json:"validations,omitempty"`
}

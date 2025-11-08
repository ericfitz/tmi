package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Addon represents an add-on in the system
type Addon struct {
	ID            uuid.UUID  `json:"id"`
	CreatedAt     time.Time  `json:"created_at"`
	Name          string     `json:"name"`
	WebhookID     uuid.UUID  `json:"webhook_id"`
	Description   string     `json:"description,omitempty"`
	Icon          string     `json:"icon,omitempty"`
	Objects       []string   `json:"objects,omitempty"`
	ThreatModelID *uuid.UUID `json:"threat_model_id,omitempty"`
}

// AddonStore defines the interface for add-on storage operations
type AddonStore interface {
	// Create creates a new add-on
	Create(ctx context.Context, addon *Addon) error

	// Get retrieves an add-on by ID
	Get(ctx context.Context, id uuid.UUID) (*Addon, error)

	// List retrieves add-ons with pagination, optionally filtered by threat model
	List(ctx context.Context, limit, offset int, threatModelID *uuid.UUID) ([]Addon, int, error)

	// Delete removes an add-on by ID
	Delete(ctx context.Context, id uuid.UUID) error

	// GetByWebhookID retrieves all add-ons associated with a webhook
	GetByWebhookID(ctx context.Context, webhookID uuid.UUID) ([]Addon, error)

	// CountActiveInvocations counts pending/in_progress invocations for an add-on
	// This will be used to block deletion when active invocations exist
	// Returns count of active invocations
	CountActiveInvocations(ctx context.Context, addonID uuid.UUID) (int, error)
}

// GlobalAddonStore is the global singleton for add-on storage
var GlobalAddonStore AddonStore

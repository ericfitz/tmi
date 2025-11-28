package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Administrator represents an administrator entry using dual foreign key pattern
type Administrator struct {
	ID                uuid.UUID  `json:"id"`
	UserInternalUUID  *uuid.UUID `json:"user_internal_uuid,omitempty"`  // Populated for user-type admins
	GroupInternalUUID *uuid.UUID `json:"group_internal_uuid,omitempty"` // Populated for group-type admins
	SubjectType       string     `json:"subject_type"`                  // "user" or "group"
	Provider          string     `json:"provider"`                      // OAuth/SAML provider
	GrantedAt         time.Time  `json:"granted_at"`
	GrantedBy         *uuid.UUID `json:"granted_by,omitempty"`
	Notes             string     `json:"notes,omitempty"`
}

// AdministratorStore defines the interface for administrator storage operations
type AdministratorStore interface {
	// Create adds a new administrator entry
	Create(ctx context.Context, admin Administrator) error

	// Delete removes an administrator entry by ID
	Delete(ctx context.Context, id uuid.UUID) error

	// List returns all administrator entries
	List(ctx context.Context) ([]Administrator, error)

	// IsAdmin checks if a user or any of their groups is an administrator
	// Checks by user UUID and provider, or by group UUIDs and provider
	IsAdmin(ctx context.Context, userUUID *uuid.UUID, provider string, groupUUIDs []uuid.UUID) (bool, error)

	// GetByPrincipal retrieves administrator entries by user or group UUID
	GetByPrincipal(ctx context.Context, userUUID *uuid.UUID, groupUUID *uuid.UUID, provider string) ([]Administrator, error)
}

// GlobalAdministratorStore is the global singleton for administrator storage
var GlobalAdministratorStore AdministratorStore

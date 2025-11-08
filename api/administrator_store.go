package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Administrator represents an administrator entry
type Administrator struct {
	UserID      uuid.UUID  `json:"user_id"`
	Subject     string     `json:"subject"`      // email for users, group name for groups
	SubjectType string     `json:"subject_type"` // "user" or "group"
	GrantedAt   time.Time  `json:"granted_at"`
	GrantedBy   *uuid.UUID `json:"granted_by,omitempty"`
	Notes       string     `json:"notes,omitempty"`
}

// AdministratorStore defines the interface for administrator storage operations
type AdministratorStore interface {
	// Create adds a new administrator entry
	Create(ctx context.Context, admin Administrator) error

	// Delete removes an administrator entry
	Delete(ctx context.Context, userID uuid.UUID, subject string, subjectType string) error

	// List returns all administrator entries
	List(ctx context.Context) ([]Administrator, error)

	// IsAdmin checks if a user (by email or UUID) or any of their groups is an administrator
	IsAdmin(ctx context.Context, userID *uuid.UUID, email string, groups []string) (bool, error)

	// GetBySubject retrieves administrator entries by subject (email or group)
	GetBySubject(ctx context.Context, subject string) ([]Administrator, error)
}

// GlobalAdministratorStore is the global singleton for administrator storage
var GlobalAdministratorStore AdministratorStore

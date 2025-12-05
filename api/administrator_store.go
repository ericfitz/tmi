package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DBAdministrator represents the internal database model for an administrator entry
// This has more fields than the API's Administrator type for internal tracking
type DBAdministrator struct {
	ID                uuid.UUID  `json:"id"`
	UserInternalUUID  *uuid.UUID `json:"user_internal_uuid,omitempty"`  // Populated for user-type admins
	UserEmail         string     `json:"user_email,omitempty"`          // Enriched field - user's email
	UserName          string     `json:"user_name,omitempty"`           // Enriched field - user's display name
	GroupInternalUUID *uuid.UUID `json:"group_internal_uuid,omitempty"` // Populated for group-type admins
	GroupName         string     `json:"group_name,omitempty"`          // Enriched field - group's name
	SubjectType       string     `json:"subject_type"`                  // "user" or "group"
	Provider          string     `json:"provider"`                      // OAuth/SAML provider
	GrantedAt         time.Time  `json:"granted_at"`
	GrantedBy         *uuid.UUID `json:"granted_by,omitempty"`
	Notes             string     `json:"notes,omitempty"`
}

// ToAPI converts DBAdministrator to API Administrator type
func (db *DBAdministrator) ToAPI() Administrator {
	admin := Administrator{
		Id:        db.ID,
		Provider:  db.Provider,
		CreatedAt: db.GrantedAt,
	}

	if db.UserInternalUUID != nil {
		admin.UserId = db.UserInternalUUID
	}

	if db.UserEmail != "" {
		admin.UserEmail = &db.UserEmail
	}

	if db.UserName != "" {
		admin.UserName = &db.UserName
	}

	if db.GroupInternalUUID != nil {
		admin.GroupId = db.GroupInternalUUID
	}

	if db.GroupName != "" {
		admin.GroupName = &db.GroupName
	}

	return admin
}

// AdministratorStore defines the interface for administrator storage operations
type AdministratorStore interface {
	// Create adds a new administrator entry
	Create(ctx context.Context, admin DBAdministrator) error

	// Delete removes an administrator entry by ID
	Delete(ctx context.Context, id uuid.UUID) error

	// List returns all administrator entries
	List(ctx context.Context) ([]DBAdministrator, error)

	// IsAdmin checks if a user or any of their groups is an administrator
	// Checks by user UUID and provider, or by group UUIDs and provider
	IsAdmin(ctx context.Context, userUUID *uuid.UUID, provider string, groupUUIDs []uuid.UUID) (bool, error)

	// GetByPrincipal retrieves administrator entries by user or group UUID
	GetByPrincipal(ctx context.Context, userUUID *uuid.UUID, groupUUID *uuid.UUID, provider string) ([]DBAdministrator, error)
}

// GlobalAdministratorStore is the global singleton for administrator storage
var GlobalAdministratorStore AdministratorStore

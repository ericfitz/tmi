package api

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// AddonDatabaseStore implements AddonStore using PostgreSQL
type AddonDatabaseStore struct {
	db *sql.DB
}

// NewAddonDatabaseStore creates a new database-backed add-on store
func NewAddonDatabaseStore(db *sql.DB) *AddonDatabaseStore {
	return &AddonDatabaseStore{db: db}
}

// Create creates a new add-on
func (s *AddonDatabaseStore) Create(ctx context.Context, addon *Addon) error {
	logger := slogging.Get()

	// Generate ID if not provided
	if addon.ID == uuid.Nil {
		addon.ID = uuid.New()
	}

	query := `
		INSERT INTO addons (id, created_at, name, webhook_id, description, icon, objects, threat_model_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at
	`

	err := s.db.QueryRowContext(ctx, query,
		addon.ID,
		addon.CreatedAt,
		addon.Name,
		addon.WebhookID,
		addon.Description,
		addon.Icon,
		pq.Array(addon.Objects),
		addon.ThreatModelID,
	).Scan(&addon.ID, &addon.CreatedAt)

	if err != nil {
		logger.Error("Failed to create add-on: name=%s, webhook_id=%s, error=%v",
			addon.Name, addon.WebhookID, err)
		return fmt.Errorf("failed to create add-on: %w", err)
	}

	logger.Info("Add-on created: id=%s, name=%s, webhook_id=%s",
		addon.ID, addon.Name, addon.WebhookID)

	return nil
}

// Get retrieves an add-on by ID
func (s *AddonDatabaseStore) Get(ctx context.Context, id uuid.UUID) (*Addon, error) {
	logger := slogging.Get()

	query := `
		SELECT id, created_at, name, webhook_id, description, icon, objects, threat_model_id
		FROM addons
		WHERE id = $1
	`

	addon := &Addon{}
	var description, icon sql.NullString
	var threatModelID sql.NullString
	var objects pq.StringArray

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&addon.ID,
		&addon.CreatedAt,
		&addon.Name,
		&addon.WebhookID,
		&description,
		&icon,
		&objects,
		&threatModelID,
	)

	if err == sql.ErrNoRows {
		logger.Debug("Add-on not found: id=%s", id)
		return nil, fmt.Errorf("add-on not found: %s", id)
	}

	if err != nil {
		logger.Error("Failed to get add-on: id=%s, error=%v", id, err)
		return nil, fmt.Errorf("failed to get add-on: %w", err)
	}

	// Handle nullable fields
	if description.Valid {
		addon.Description = description.String
	}
	if icon.Valid {
		addon.Icon = icon.String
	}
	if threatModelID.Valid {
		tmID, err := uuid.Parse(threatModelID.String)
		if err == nil {
			addon.ThreatModelID = &tmID
		}
	}
	if objects != nil {
		addon.Objects = []string(objects)
	}

	logger.Debug("Retrieved add-on: id=%s, name=%s", addon.ID, addon.Name)

	return addon, nil
}

// List retrieves add-ons with pagination, optionally filtered by threat model
func (s *AddonDatabaseStore) List(ctx context.Context, limit, offset int, threatModelID *uuid.UUID) ([]Addon, int, error) {
	logger := slogging.Get()

	// Build query with optional threat model filter
	var query string
	var countQuery string
	var args []interface{}

	if threatModelID != nil {
		query = `
			SELECT id, created_at, name, webhook_id, description, icon, objects, threat_model_id
			FROM addons
			WHERE threat_model_id = $1 OR threat_model_id IS NULL
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		countQuery = `
			SELECT COUNT(*)
			FROM addons
			WHERE threat_model_id = $1 OR threat_model_id IS NULL
		`
		args = []interface{}{threatModelID, limit, offset}
	} else {
		query = `
			SELECT id, created_at, name, webhook_id, description, icon, objects, threat_model_id
			FROM addons
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2
		`
		countQuery = `SELECT COUNT(*) FROM addons`
		args = []interface{}{limit, offset}
	}

	// Get total count
	var total int
	var countArgs []interface{}
	if threatModelID != nil {
		countArgs = []interface{}{threatModelID}
	}
	err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total)
	if err != nil {
		logger.Error("Failed to count add-ons: error=%v", err)
		return nil, 0, fmt.Errorf("failed to count add-ons: %w", err)
	}

	// Get add-ons
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		logger.Error("Failed to list add-ons: error=%v", err)
		return nil, 0, fmt.Errorf("failed to list add-ons: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var addons []Addon
	for rows.Next() {
		var addon Addon
		var description, icon sql.NullString
		var threatModelIDStr sql.NullString
		var objects pq.StringArray

		err := rows.Scan(
			&addon.ID,
			&addon.CreatedAt,
			&addon.Name,
			&addon.WebhookID,
			&description,
			&icon,
			&objects,
			&threatModelIDStr,
		)
		if err != nil {
			logger.Error("Failed to scan add-on row: %v", err)
			return nil, 0, fmt.Errorf("failed to scan add-on: %w", err)
		}

		// Handle nullable fields
		if description.Valid {
			addon.Description = description.String
		}
		if icon.Valid {
			addon.Icon = icon.String
		}
		if threatModelIDStr.Valid {
			tmID, err := uuid.Parse(threatModelIDStr.String)
			if err == nil {
				addon.ThreatModelID = &tmID
			}
		}
		if objects != nil {
			addon.Objects = []string(objects)
		}

		addons = append(addons, addon)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating add-on rows: %v", err)
		return nil, 0, fmt.Errorf("error iterating add-ons: %w", err)
	}

	logger.Debug("Listed %d add-ons (total: %d, limit: %d, offset: %d)",
		len(addons), total, limit, offset)

	return addons, total, nil
}

// Delete removes an add-on by ID
func (s *AddonDatabaseStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	query := `DELETE FROM addons WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		logger.Error("Failed to delete add-on: id=%s, error=%v", id, err)
		return fmt.Errorf("failed to delete add-on: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected for delete: %v", err)
		return fmt.Errorf("failed to verify delete: %w", err)
	}

	if rowsAffected == 0 {
		logger.Debug("Add-on not found for delete: id=%s", id)
		return fmt.Errorf("add-on not found: %s", id)
	}

	logger.Info("Add-on deleted: id=%s", id)

	return nil
}

// GetByWebhookID retrieves all add-ons associated with a webhook
func (s *AddonDatabaseStore) GetByWebhookID(ctx context.Context, webhookID uuid.UUID) ([]Addon, error) {
	logger := slogging.Get()

	query := `
		SELECT id, created_at, name, webhook_id, description, icon, objects, threat_model_id
		FROM addons
		WHERE webhook_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, webhookID)
	if err != nil {
		logger.Error("Failed to get add-ons by webhook_id=%s: %v", webhookID, err)
		return nil, fmt.Errorf("failed to get add-ons by webhook: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var addons []Addon
	for rows.Next() {
		var addon Addon
		var description, icon sql.NullString
		var threatModelIDStr sql.NullString
		var objects pq.StringArray

		err := rows.Scan(
			&addon.ID,
			&addon.CreatedAt,
			&addon.Name,
			&addon.WebhookID,
			&description,
			&icon,
			&objects,
			&threatModelIDStr,
		)
		if err != nil {
			logger.Error("Failed to scan add-on row: %v", err)
			return nil, fmt.Errorf("failed to scan add-on: %w", err)
		}

		// Handle nullable fields
		if description.Valid {
			addon.Description = description.String
		}
		if icon.Valid {
			addon.Icon = icon.String
		}
		if threatModelIDStr.Valid {
			tmID, err := uuid.Parse(threatModelIDStr.String)
			if err == nil {
				addon.ThreatModelID = &tmID
			}
		}
		if objects != nil {
			addon.Objects = []string(objects)
		}

		addons = append(addons, addon)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating add-on rows: %v", err)
		return nil, fmt.Errorf("error iterating add-ons: %w", err)
	}

	logger.Debug("Found %d add-ons for webhook_id=%s", len(addons), webhookID)

	return addons, nil
}

// CountActiveInvocations counts pending/in_progress invocations for an add-on
// This implementation is a placeholder - it will be fully implemented when we add Redis invocation store
func (s *AddonDatabaseStore) CountActiveInvocations(ctx context.Context, addonID uuid.UUID) (int, error) {
	logger := slogging.Get()

	// TODO: This will be implemented to query Redis for active invocations
	// For now, return 0 to allow deletion
	logger.Debug("CountActiveInvocations called for addon_id=%s (placeholder implementation)", addonID)

	return 0, nil
}

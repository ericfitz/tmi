package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/uuidgen"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v4/stdlib"
)

// DatabaseStore provides a database-backed store implementation
type DatabaseStore[T any] struct {
	db         *sql.DB
	tableName  string
	entityType string
}

// NewDatabaseStore creates a new database-backed store
func NewDatabaseStore[T any](database *sql.DB, tableName, entityType string) *DatabaseStore[T] {
	return &DatabaseStore[T]{
		db:         database,
		tableName:  tableName,
		entityType: entityType,
	}
}

// ThreatModelDatabaseStore handles threat model database operations
type ThreatModelDatabaseStore struct {
	db    *sql.DB
	mutex sync.RWMutex
}

// NewThreatModelDatabaseStore creates a new threat model database store
func NewThreatModelDatabaseStore(database *sql.DB) *ThreatModelDatabaseStore {
	return &ThreatModelDatabaseStore{
		db: database,
	}
}

// GetDB returns the underlying database connection
func (s *ThreatModelDatabaseStore) GetDB() *sql.DB {
	return s.db
}

// resolveUserIdentifierToUUID attempts to resolve a user identifier to an internal_uuid
// It tries in order:
// 1. If the value is a valid UUID, check if it exists as internal_uuid
// 2. Try to match as provider_user_id for any provider
// 3. Try to match as email
// Returns the internal_uuid or an error if the user cannot be found
func (s *ThreatModelDatabaseStore) resolveUserIdentifierToUUID(tx *sql.Tx, identifier string) (string, error) {
	// Step 1: Check if it's already a valid internal_uuid
	if _, err := uuid.Parse(identifier); err == nil {
		var existsUUID string
		err := tx.QueryRow(`SELECT internal_uuid FROM users WHERE internal_uuid = $1`, identifier).Scan(&existsUUID)
		if err == nil {
			return existsUUID, nil
		}
		// Not found as internal_uuid, continue to other checks
	}

	// Step 2: Try as provider_user_id (check all providers)
	var internalUUID string
	err := tx.QueryRow(`SELECT internal_uuid FROM users WHERE provider_user_id = $1`, identifier).Scan(&internalUUID)
	if err == nil {
		return internalUUID, nil
	}

	// Step 3: Try as email
	err = tx.QueryRow(`SELECT internal_uuid FROM users WHERE email = $1`, identifier).Scan(&internalUUID)
	if err == nil {
		return internalUUID, nil
	}

	// Not found by any method
	return "", fmt.Errorf("user not found with identifier: %s", identifier)
}

// resolveGroupToUUID attempts to resolve a group identifier to an internal_uuid
// It looks up the group by provider and group_name in the groups table
// Returns the internal_uuid or an error if the group cannot be found
func (s *ThreatModelDatabaseStore) resolveGroupToUUID(tx *sql.Tx, groupName string, idp *string) (string, error) {
	provider := "*"
	if idp != nil && *idp != "" {
		provider = *idp
	}

	var internalUUID string
	err := tx.QueryRow(`
		SELECT internal_uuid
		FROM groups
		WHERE provider = $1 AND group_name = $2
	`, provider, groupName).Scan(&internalUUID)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("group not found: %s@%s", groupName, provider)
	}
	return internalUUID, err
}

// ensureGroupExists creates a group entry if it doesn't exist and returns its internal_uuid
func (s *ThreatModelDatabaseStore) ensureGroupExists(tx *sql.Tx, groupName string, idp *string) (string, error) {
	provider := "*"
	if idp != nil && *idp != "" {
		provider = *idp
	}

	// Try to insert, get UUID either way
	var internalUUID uuid.UUID
	query := `
		INSERT INTO groups (provider, group_name, name, usage_count)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (provider, group_name)
		DO UPDATE SET
			last_used = CURRENT_TIMESTAMP,
			usage_count = groups.usage_count + 1
		RETURNING internal_uuid
	`

	displayName := groupName // Use group_name as display name by default
	err := tx.QueryRow(query, provider, groupName, displayName).Scan(&internalUUID)
	if err != nil {
		return "", fmt.Errorf("failed to ensure group exists: %w", err)
	}

	return internalUUID.String(), nil
}

// Get retrieves a threat model by ID
func (s *ThreatModelDatabaseStore) Get(id string) (ThreatModel, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	slogging.Get().GetSlogger().Debug("ThreatModelDatabaseStore.Get() called", "id", id)

	// Check if database connection is nil
	if s.db == nil {
		slogging.Get().Error("Database connection is nil")
		return ThreatModel{}, fmt.Errorf("database connection is nil")
	}

	// Test database connectivity
	if err := s.db.Ping(); err != nil {
		slogging.Get().GetSlogger().Error("Database ping failed", "error", err)
		return ThreatModel{}, fmt.Errorf("database ping failed: %w", err)
	}
	slogging.Get().Debug("Database ping successful")

	// Try to validate the UUID format first
	if _, err := uuid.Parse(id); err != nil {
		slogging.Get().GetSlogger().Error("Invalid UUID format", "id", id, "error", err)
		return ThreatModel{}, fmt.Errorf("invalid UUID format: %w", err)
	}
	slogging.Get().GetSlogger().Debug("UUID format validation passed", "id", id)

	// Start transaction for consistent reads including enrichment
	tx, err := s.db.Begin()
	if err != nil {
		return ThreatModel{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		} else {
			_ = tx.Commit()
		}
	}()

	var tm ThreatModel
	var tmUUID uuid.UUID
	var name, ownerInternalUUID, createdByInternalUUID string
	var description, issueUrl *string
	var threatModelFramework string
	var status *string
	var statusUpdated *time.Time
	var createdAt, modifiedAt time.Time

	query := `
		SELECT id, name, description, owner_internal_uuid, created_by_internal_uuid,
		       threat_model_framework, issue_uri, status, status_updated,
		       created_at, modified_at
		FROM threat_models
		WHERE id = $1`

	slogging.Get().GetSlogger().Debug("Executing query", "query", query)
	slogging.Get().GetSlogger().Debug("Query parameter", "id", id, "type", fmt.Sprintf("%T", id), "length", len(id))

	err = tx.QueryRow(query, id).Scan(
		&tmUUID, &name, &description, &ownerInternalUUID, &createdByInternalUUID,
		&threatModelFramework, &issueUrl, &status, &statusUpdated,
		&createdAt, &modifiedAt,
	)

	slogging.Get().GetSlogger().Debug("Query execution completed", "error", err)

	if err != nil {
		if err == sql.ErrNoRows {
			slogging.Get().GetSlogger().Debug("No rows found", "id", id)
			return tm, fmt.Errorf("threat model with ID %s not found", id)
		}
		slogging.Get().GetSlogger().Error("Database error (not ErrNoRows)", "error", err)
		return tm, fmt.Errorf("failed to get threat model: %w", err)
	}

	// Enrich owner
	owner, err := enrichUserPrincipal(tx, ownerInternalUUID)
	if err != nil {
		return tm, fmt.Errorf("failed to enrich owner: %w", err)
	}
	if owner == nil {
		return tm, fmt.Errorf("owner user not found for threat model %s", id)
	}

	// Enrich created_by
	createdBy, err := enrichUserPrincipal(tx, createdByInternalUUID)
	if err != nil {
		return tm, fmt.Errorf("failed to enrich created_by: %w", err)
	}
	if createdBy == nil {
		return tm, fmt.Errorf("created_by user not found for threat model %s", id)
	}

	slogging.Get().GetSlogger().Debug("Query successful! Retrieved threat model", "id", tmUUID.String(), "name", name, "owner", owner.Email)

	// Load authorization (pass tx for consistent read)
	authorization, err := s.loadAuthorization(id, tx)
	if err != nil {
		return tm, fmt.Errorf("failed to load authorization: %w", err)
	}

	slogging.Get().Debug("[DB-STORE] Loaded %d authorization entries for threat model %s", len(authorization), id)

	// Load metadata
	metadata, err := s.loadMetadata(id)
	if err != nil {
		return tm, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Load threats
	threats, err := s.loadThreats(id)
	if err != nil {
		return tm, fmt.Errorf("failed to load threats: %w", err)
	}

	// Load diagrams dynamically from DiagramStore to ensure single source of truth
	diagrams, err := s.loadDiagramsDynamically(id)
	if err != nil {
		return tm, fmt.Errorf("failed to load diagrams: %w", err)
	}

	// Set default framework if empty
	framework := threatModelFramework
	if threatModelFramework == "" {
		framework = "STRIDE" // default
	}

	tm = ThreatModel{
		Id:                   &tmUUID,
		Name:                 name,
		Description:          description,
		Owner:                *owner,
		CreatedBy:            createdBy,
		ThreatModelFramework: framework,
		IssueUri:             issueUrl,
		Status:               status,
		StatusUpdated:        statusUpdated,
		CreatedAt:            &createdAt,
		ModifiedAt:           &modifiedAt,
		Authorization:        authorization,
		Metadata:             &metadata,
		Threats:              &threats,
		Diagrams:             diagrams,
	}

	return tm, nil
}

// List returns filtered and paginated threat models
func (s *ThreatModelDatabaseStore) List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []ThreatModel

	query := `
		SELECT id, name, description, owner_internal_uuid, created_by_internal_uuid,
		       threat_model_framework, issue_uri, status, status_updated,
		       created_at, modified_at
		FROM threat_models
		ORDER BY created_at DESC`

	rows, err := s.db.Query(query)
	if err != nil {
		return results
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	for rows.Next() {
		var tm ThreatModel
		var uuid uuid.UUID
		var name string
		var ownerInternalUUID, createdByInternalUUID string
		var description, issueUrl *string
		var threatModelFramework string
		var status *string
		var statusUpdated *time.Time
		var createdAt, modifiedAt time.Time

		err := rows.Scan(
			&uuid, &name, &description, &ownerInternalUUID, &createdByInternalUUID,
			&threatModelFramework, &issueUrl, &status, &statusUpdated,
			&createdAt, &modifiedAt,
		)
		if err != nil {
			continue
		}

		// Begin transaction for consistent enrichment
		tx, err := s.db.Begin()
		if err != nil {
			continue
		}

		// Enrich owner and created_by from database
		owner, err := enrichUserPrincipal(tx, ownerInternalUUID)
		if err != nil || owner == nil {
			_ = tx.Rollback()
			continue
		}

		createdBy, err := enrichUserPrincipal(tx, createdByInternalUUID)
		if err != nil {
			_ = tx.Rollback()
			continue
		}

		_ = tx.Commit()

		// Load authorization for filtering (no tx - independent call)
		authorization, err := s.loadAuthorization(uuid.String(), nil)
		if err != nil {
			continue
		}

		// Set default framework if empty
		framework := threatModelFramework
		if threatModelFramework == "" {
			framework = "STRIDE" // default
		}

		tm = ThreatModel{
			Id:                   &uuid,
			Name:                 name,
			Description:          description,
			Owner:                *owner,
			CreatedBy:            createdBy,
			ThreatModelFramework: framework,
			IssueUri:             issueUrl,
			Status:               status,
			StatusUpdated:        statusUpdated,
			CreatedAt:            &createdAt,
			ModifiedAt:           &modifiedAt,
			Authorization:        authorization,
		}

		// Apply filter if provided
		if filter == nil || filter(tm) {
			results = append(results, tm)
		}
	}

	// Apply pagination
	if offset >= len(results) {
		return []ThreatModel{}
	}

	end := offset + limit
	if end > len(results) || limit <= 0 {
		end = len(results)
	}

	return results[offset:end]
}

// ListWithCounts returns filtered and paginated threat models with count information
func (s *ThreatModelDatabaseStore) ListWithCounts(offset, limit int, filter func(ThreatModel) bool) []ThreatModelWithCounts {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []ThreatModelWithCounts

	query := `
		SELECT id, name, description, owner_internal_uuid, created_by_internal_uuid,
		       threat_model_framework, issue_uri, status, status_updated,
		       created_at, modified_at
		FROM threat_models
		ORDER BY created_at DESC`

	rows, err := s.db.Query(query)
	if err != nil {
		return results
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	for rows.Next() {
		var tm ThreatModel
		var uuid uuid.UUID
		var name string
		var ownerInternalUUID, createdByInternalUUID string
		var description, issueUrl *string
		var threatModelFramework string
		var status *string
		var statusUpdated *time.Time
		var createdAt, modifiedAt time.Time

		err := rows.Scan(
			&uuid, &name, &description, &ownerInternalUUID, &createdByInternalUUID,
			&threatModelFramework, &issueUrl, &status, &statusUpdated,
			&createdAt, &modifiedAt,
		)
		if err != nil {
			continue
		}

		// Begin transaction for consistent enrichment
		tx, err := s.db.Begin()
		if err != nil {
			continue
		}

		// Enrich owner and created_by from database
		owner, err := enrichUserPrincipal(tx, ownerInternalUUID)
		if err != nil || owner == nil {
			_ = tx.Rollback()
			continue
		}

		createdBy, err := enrichUserPrincipal(tx, createdByInternalUUID)
		if err != nil {
			_ = tx.Rollback()
			continue
		}

		_ = tx.Commit()

		// Load authorization for filtering (no tx - independent call)
		authorization, err := s.loadAuthorization(uuid.String(), nil)
		if err != nil {
			continue
		}

		// Set default framework if empty
		framework := threatModelFramework
		if threatModelFramework == "" {
			framework = "STRIDE" // default
		}

		tm = ThreatModel{
			Id:                   &uuid,
			Name:                 name,
			Description:          description,
			Owner:                *owner,
			CreatedBy:            createdBy,
			ThreatModelFramework: framework,
			IssueUri:             issueUrl,
			Status:               status,
			StatusUpdated:        statusUpdated,
			CreatedAt:            &createdAt,
			ModifiedAt:           &modifiedAt,
			Authorization:        authorization,
		}

		// Apply filter if provided
		if filter == nil || filter(tm) {
			// Calculate actual counts instead of using potentially stale database values
			actualDiagramCount := s.calculateDiagramCount(uuid.String())
			actualThreatCount := s.calculateThreatCount(uuid.String())
			actualDocumentCount := s.calculateDocumentCount(uuid.String())
			actualRepositoryCount := s.calculateRepositoryCount(uuid.String())
			actualNoteCount := s.calculateNoteCount(uuid.String())
			actualAssetCount := s.calculateAssetCount(uuid.String())

			results = append(results, ThreatModelWithCounts{
				ThreatModel:   tm,
				DocumentCount: actualDocumentCount,
				SourceCount:   actualRepositoryCount,
				DiagramCount:  actualDiagramCount,
				ThreatCount:   actualThreatCount,
				NoteCount:     actualNoteCount,
				AssetCount:    actualAssetCount,
			})
		}
	}

	// Apply pagination
	if offset >= len(results) {
		return []ThreatModelWithCounts{}
	}

	end := offset + limit
	if end > len(results) || limit <= 0 {
		end = len(results)
	}

	return results[offset:end]
}

// calculateDiagramCount counts the actual number of diagrams for a threat model
func (s *ThreatModelDatabaseStore) calculateDiagramCount(threatModelId string) int {
	query := `SELECT COUNT(*) FROM diagrams WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRow(query, threatModelId).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// calculateThreatCount counts the actual number of threats for a threat model
func (s *ThreatModelDatabaseStore) calculateThreatCount(threatModelId string) int {
	query := `SELECT COUNT(*) FROM threats WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRow(query, threatModelId).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// calculateDocumentCount counts the actual number of documents for a threat model
func (s *ThreatModelDatabaseStore) calculateDocumentCount(threatModelId string) int {
	query := `SELECT COUNT(*) FROM documents WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRow(query, threatModelId).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// calculateRepositoryCount counts the actual number of repositories for a threat model
func (s *ThreatModelDatabaseStore) calculateRepositoryCount(threatModelId string) int {
	query := `SELECT COUNT(*) FROM repositories WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRow(query, threatModelId).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// calculateNoteCount counts the actual number of notes for a threat model
func (s *ThreatModelDatabaseStore) calculateNoteCount(threatModelId string) int {
	query := `SELECT COUNT(*) FROM notes WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRow(query, threatModelId).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// calculateAssetCount counts the actual number of assets for a threat model
func (s *ThreatModelDatabaseStore) calculateAssetCount(threatModelId string) int {
	query := `SELECT COUNT(*) FROM assets WHERE threat_model_id = $1`
	var count int
	err := s.db.QueryRow(query, threatModelId).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// Create adds a new threat model
func (s *ThreatModelDatabaseStore) Create(item ThreatModel, idSetter func(ThreatModel, string) ThreatModel) (ThreatModel, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Begin transaction
	tx, err := s.db.Begin()
	if err != nil {
		return item, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Error rolling back transaction, but don't fail the operation
			_ = err
		}
	}()

	// Generate ID if not set
	id := uuid.New()
	if idSetter != nil {
		item = idSetter(item, id.String())
	}

	// Get framework value
	framework := item.ThreatModelFramework
	if framework == "" {
		framework = "STRIDE" // default
	}

	// Set status_updated if status is provided
	var statusUpdated *time.Time
	if item.Status != nil && len(*item.Status) > 0 {
		now := time.Now().UTC()
		statusUpdated = &now
	}

	// Resolve owner identifier (UUID, provider_user_id, or email) to internal_uuid
	ownerUUID, err := s.resolveUserIdentifierToUUID(tx, item.Owner.ProviderId)
	if err != nil {
		return item, fmt.Errorf("failed to resolve owner identifier %s: %w", item.Owner.ProviderId, err)
	}

	// Resolve created_by identifier to internal_uuid
	createdByUUID, err := s.resolveUserIdentifierToUUID(tx, item.CreatedBy.ProviderId)
	if err != nil {
		return item, fmt.Errorf("failed to resolve created_by identifier %s: %w", item.CreatedBy.ProviderId, err)
	}

	// Insert threat model
	query := `
		INSERT INTO threat_models (id, name, description, owner_internal_uuid, created_by_internal_uuid,
		                          threat_model_framework, issue_uri, status, status_updated,
		                          created_at, modified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err = tx.Exec(query,
		id, item.Name, item.Description, ownerUUID, createdByUUID,
		framework, item.IssueUri, item.Status, statusUpdated,
		item.CreatedAt, item.ModifiedAt,
	)
	if err != nil {
		return item, fmt.Errorf("failed to insert threat model: %w", err)
	}

	// Insert authorization entries
	if err := s.saveAuthorizationTx(tx, id.String(), item.Authorization); err != nil {
		return item, fmt.Errorf("failed to save authorization: %w", err)
	}

	// Insert metadata if present
	if item.Metadata != nil && len(*item.Metadata) > 0 {
		if err := s.saveMetadataTx(tx, id.String(), *item.Metadata); err != nil {
			return item, fmt.Errorf("failed to save metadata: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return item, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return item, nil
}

// Update modifies an existing threat model
func (s *ThreatModelDatabaseStore) Update(id string, item ThreatModel) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Begin transaction
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			// Error rolling back transaction, but don't fail the operation
			_ = err
		}
	}()

	// Get framework value
	framework := item.ThreatModelFramework
	if framework == "" {
		framework = "STRIDE" // default
	}

	// Check if status has changed to determine if we should update status_updated
	var oldStatus *string
	err = tx.QueryRow("SELECT status FROM threat_models WHERE id = $1", id).Scan(&oldStatus)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get current status: %w", err)
	}

	// Determine if status changed (simple string comparison)
	statusChanged := false
	newStatus := item.Status
	if newStatus == nil && oldStatus != nil {
		statusChanged = true
	} else if newStatus != nil && oldStatus == nil {
		statusChanged = true
	} else if newStatus != nil && oldStatus != nil {
		if *newStatus != *oldStatus {
			statusChanged = true
		}
	}

	// Set status_updated if status changed
	var statusUpdated *time.Time
	if statusChanged {
		now := time.Now().UTC()
		statusUpdated = &now
	}

	// Resolve owner identifier (UUID, provider_user_id, or email) to internal_uuid
	ownerUUID, err := s.resolveUserIdentifierToUUID(tx, item.Owner.ProviderId)
	if err != nil {
		return fmt.Errorf("failed to resolve owner identifier %s: %w", item.Owner, err)
	}

	// Resolve created_by identifier to internal_uuid
	var createdByUUID string
	if item.CreatedBy != nil {
		createdByUUID, err = s.resolveUserIdentifierToUUID(tx, item.CreatedBy.ProviderId)
		if err != nil {
			return fmt.Errorf("failed to resolve created_by identifier %s: %w", item.CreatedBy, err)
		}
	} else {
		// If CreatedBy is nil, use the owner as creator (fallback)
		createdByUUID = ownerUUID
	}

	// Update threat model
	query := `
		UPDATE threat_models
		SET name = $2, description = $3, owner_internal_uuid = $4, created_by_internal_uuid = $5,
		    threat_model_framework = $6, issue_uri = $7, status = $8, status_updated = $9,
		    modified_at = $10
		WHERE id = $1`

	result, err := tx.Exec(query,
		id, item.Name, item.Description, ownerUUID, createdByUUID,
		framework, item.IssueUri, item.Status, statusUpdated,
		item.ModifiedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update threat model: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("threat model with ID %s not found", id)
	}

	// Update authorization
	slogging.Get().Debug("[DB-STORE] Updating authorization for threat model %s with %d entries", id, len(item.Authorization))
	for i, auth := range item.Authorization {
		slogging.Get().Debug("[DB-STORE]   Entry %d: type=%s, provider=%s, provider_id=%s, role=%s",
			i, auth.PrincipalType, auth.Provider, auth.ProviderId, auth.Role)
	}
	if err := s.updateAuthorizationTx(tx, id, item.Authorization); err != nil {
		return fmt.Errorf("failed to update authorization: %w", err)
	}

	// Update metadata
	if item.Metadata != nil {
		if err := s.updateMetadataTx(tx, id, *item.Metadata); err != nil {
			return fmt.Errorf("failed to update metadata: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Delete removes a threat model
func (s *ThreatModelDatabaseStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `DELETE FROM threat_models WHERE id = $1`
	result, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete threat model: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("threat model with ID %s not found", id)
	}

	return nil
}

// Count returns the total number of threat models
func (s *ThreatModelDatabaseStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int
	query := `SELECT COUNT(*) FROM threat_models`
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// Helper methods for loading related data

func (s *ThreatModelDatabaseStore) loadAuthorization(threatModelId string, tx *sql.Tx) ([]Authorization, error) {
	// Use provided transaction or create a new one
	var localTx *sql.Tx
	var err error

	if tx == nil {
		// Start a transaction for consistent reads
		localTx, err = s.db.Begin()
		if err != nil {
			return nil, fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() {
			if err != nil {
				_ = localTx.Rollback()
			} else {
				_ = localTx.Commit()
			}
		}()
	} else {
		localTx = tx
	}

	query := `
		SELECT
			user_internal_uuid,
			group_internal_uuid,
			subject_type,
			role
		FROM threat_model_access
		WHERE threat_model_id = $1
		ORDER BY role DESC`

	slogging.Get().Debug("[DB-STORE] loadAuthorization: Querying authorization for threat model %s", threatModelId)
	rows, err := localTx.Query(query, threatModelId)
	if err != nil {
		slogging.Get().Error("[DB-STORE] loadAuthorization: Query failed: %v", err)
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	// Initialize as empty slice to ensure JSON marshals to [] instead of null
	authorization := []Authorization{}
	rowCount := 0
	for rows.Next() {
		rowCount++
		var userUUID, groupUUID sql.NullString
		var subjectTypeStr, roleStr string
		if err := rows.Scan(&userUUID, &groupUUID, &subjectTypeStr, &roleStr); err != nil {
			slogging.Get().Error("[DB-STORE] loadAuthorization: Failed to scan row %d: %v", rowCount, err)
			continue
		}

		slogging.Get().Debug("[DB-STORE] loadAuthorization: Row %d - user_uuid=%v, group_uuid=%v, subject_type=%s, role=%s",
			rowCount, userUUID, groupUUID, subjectTypeStr, roleStr)

		role := AuthorizationRole(roleStr)

		// Enrich based on subject type
		if subjectTypeStr == "user" && userUUID.Valid {
			slogging.Get().Debug("[DB-STORE] loadAuthorization: Enriching user principal with internal_uuid=%s", userUUID.String)
			user, enrichErr := enrichUserPrincipal(localTx, userUUID.String)
			if enrichErr != nil {
				// Log but continue - graceful degradation
				slogging.Get().Warn("[DB-STORE] loadAuthorization: Failed to enrich user principal %s: %v", userUUID.String, enrichErr)
				continue
			}
			if user == nil {
				// User not found - skip this authorization entry
				slogging.Get().Warn("[DB-STORE] loadAuthorization: User principal %s not found - SKIPPING authorization entry", userUUID.String)
				continue
			}
			slogging.Get().Debug("[DB-STORE] loadAuthorization: Successfully enriched user - provider=%s, provider_id=%s", user.Provider, user.ProviderId)

			// Create Authorization from User (which extends Principal)
			auth := Authorization{
				PrincipalType: AuthorizationPrincipalType(user.PrincipalType),
				Provider:      user.Provider,
				ProviderId:    user.ProviderId,
				Role:          role,
			}
			// Set optional fields
			auth.DisplayName = &user.DisplayName
			emailPtr := &user.Email
			auth.Email = emailPtr

			authorization = append(authorization, auth)

		} else if subjectTypeStr == "group" && groupUUID.Valid {
			principal, enrichErr := enrichGroupPrincipal(localTx, groupUUID.String)
			if enrichErr != nil {
				// Log but continue - graceful degradation
				slogging.Get().Warn("Failed to enrich group principal: %v", enrichErr)
				continue
			}
			if principal == nil {
				// Group not found - skip this authorization entry
				continue
			}

			// Create Authorization from Principal
			auth := Authorization{
				PrincipalType: AuthorizationPrincipalType(principal.PrincipalType),
				Provider:      principal.Provider,
				ProviderId:    principal.ProviderId,
				Role:          role,
			}
			// Set optional fields
			auth.DisplayName = principal.DisplayName
			auth.Email = principal.Email

			authorization = append(authorization, auth)
		}
	}

	return authorization, nil
}

func (s *ThreatModelDatabaseStore) loadMetadata(threatModelId string) ([]Metadata, error) {
	query := `
		SELECT key, value 
		FROM metadata 
		WHERE entity_type = 'threat_model' AND entity_id = $1`

	rows, err := s.db.Query(query, threatModelId)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	var metadata []Metadata
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}

		metadata = append(metadata, Metadata{
			Key:   key,
			Value: value,
		})
	}

	return metadata, nil
}

// loadThreatMetadata loads metadata for a specific threat from the metadata table
func (s *ThreatModelDatabaseStore) loadThreatMetadata(threatId string) ([]Metadata, error) {
	query := `
		SELECT key, value 
		FROM metadata 
		WHERE entity_type = 'threat' AND entity_id = $1
		ORDER BY key ASC`

	rows, err := s.db.Query(query, threatId)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	var metadata []Metadata
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}

		metadata = append(metadata, Metadata{
			Key:   key,
			Value: value,
		})
	}

	return metadata, nil
}

func (s *ThreatModelDatabaseStore) loadThreats(threatModelId string) ([]Threat, error) {
	query := `
		SELECT id, name, description, severity, mitigation, diagram_id, cell_id, asset_id,
		       priority, mitigated, status, threat_type, score, issue_uri,
		       created_at, modified_at
		FROM threats
		WHERE threat_model_id = $1`

	rows, err := s.db.Query(query, threatModelId)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	// Initialize as empty slice to ensure JSON marshals to [] instead of null
	threats := []Threat{}
	for rows.Next() {
		var id, threatModelUuid uuid.UUID
		var name, threatType string
		var priority, status *string
		var description, severityStr, mitigation *string
		var diagramIdStr, cellIdStr, assetIdStr *string
		var issueUrl *string
		var score *float64
		var mitigated *bool
		var createdAt, modifiedAt time.Time

		if err := rows.Scan(&id, &name, &description, &severityStr, &mitigation, &diagramIdStr, &cellIdStr, &assetIdStr,
			&priority, &mitigated, &status, &threatType, &score, &issueUrl,
			&createdAt, &modifiedAt); err != nil {
			continue
		}

		// Convert threat model ID
		threatModelUuid, _ = uuid.Parse(threatModelId)

		// Convert severity
		var severity *string
		if severityStr != nil && *severityStr != "" {
			severity = severityStr
		}

		// Convert diagram_id, cell_id, and asset_id from strings to UUIDs
		var diagramId, cellId, assetId *uuid.UUID
		if diagramIdStr != nil && *diagramIdStr != "" {
			if diagId, err := uuid.Parse(*diagramIdStr); err == nil {
				diagramId = &diagId
			}
		}
		if cellIdStr != nil && *cellIdStr != "" {
			if cId, err := uuid.Parse(*cellIdStr); err == nil {
				cellId = &cId
			}
		}
		if assetIdStr != nil && *assetIdStr != "" {
			if aId, err := uuid.Parse(*assetIdStr); err == nil {
				assetId = &aId
			}
		}

		// Convert score from float64 to float32
		var scoreFloat32 *float32
		if score != nil {
			score32 := float32(*score)
			scoreFloat32 = &score32
		}

		// Load metadata for this threat from the metadata table
		threatMetadata, err := s.loadThreatMetadata(id.String())
		if err != nil {
			// Log error but don't fail the entire operation - just set empty metadata
			threatMetadata = []Metadata{}
		}
		metadata := &threatMetadata

		threats = append(threats, Threat{
			Id:            &id,
			Name:          name,
			Description:   description,
			Severity:      severity,
			Mitigation:    mitigation,
			DiagramId:     diagramId,
			CellId:        cellId,
			AssetId:       assetId,
			Priority:      priority,
			Mitigated:     mitigated,
			Status:        status,
			ThreatType:    threatType,
			Score:         scoreFloat32,
			IssueUri:      issueUrl,
			Metadata:      metadata,
			CreatedAt:     &createdAt,
			ModifiedAt:    &modifiedAt,
			ThreatModelId: &threatModelUuid,
		})
	}

	return threats, nil
}

// loadDiagramsDynamically loads diagrams using the DiagramStore for single source of truth
func (s *ThreatModelDatabaseStore) loadDiagramsDynamically(threatModelId string) (*[]Diagram, error) {
	// First, get diagram IDs for this threat model
	query := `SELECT id FROM diagrams WHERE threat_model_id = $1 ORDER BY created_at`
	rows, err := s.db.Query(query, threatModelId)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	var diagramIds []string
	for rows.Next() {
		var diagramUuid uuid.UUID
		if err := rows.Scan(&diagramUuid); err != nil {
			continue
		}
		diagramIds = append(diagramIds, diagramUuid.String())
	}

	if len(diagramIds) == 0 {
		// Return pointer to empty slice to ensure JSON marshals to [] instead of null
		emptySlice := []Diagram{}
		return &emptySlice, nil
	}

	// Load each diagram from the DiagramStore to ensure single source of truth
	// Initialize as empty slice to ensure JSON marshals to [] instead of null
	diagrams := []Diagram{}
	for _, diagramId := range diagramIds {
		diagram, err := DiagramStore.Get(diagramId)
		if err != nil {
			// Skip missing diagrams but continue with others
			continue
		}

		// Ensure backward compatibility for existing diagrams
		if diagram.Image == nil {
			diagram.Image = &struct {
				Svg          *[]byte `json:"svg,omitempty"`
				UpdateVector *int64  `json:"update_vector,omitempty"`
			}{}
		}

		var diagramUnion Diagram
		if err := diagramUnion.FromDfdDiagram(diagram); err != nil {
			continue
		}
		diagrams = append(diagrams, diagramUnion)
	}

	return &diagrams, nil
}

func (s *ThreatModelDatabaseStore) saveAuthorizationTx(tx *sql.Tx, threatModelId string, authorization []Authorization) error {
	if len(authorization) == 0 {
		return nil
	}

	for _, auth := range authorization {
		// Determine subject type string for database
		subjectTypeStr := "user"
		if auth.PrincipalType == AuthorizationPrincipalTypeGroup {
			subjectTypeStr = "group"
		}

		// Determine which FK column to populate based on subject type
		var userUUID, groupUUID interface{}

		switch subjectTypeStr {
		case "user":
			// Resolve user identifier to internal_uuid
			resolvedUUID, err := s.resolveUserIdentifierToUUID(tx, auth.ProviderId)
			if err != nil {
				// If resolution fails, keep the original subject value
				slogging.Get().Debug("Could not resolve user identifier %s to internal_uuid, using as-is: %v", auth.ProviderId, err)
				userUUID = auth.ProviderId
			} else {
				userUUID = resolvedUUID
			}
			groupUUID = nil
		case "group":
			// Handle "everyone" pseudo-group specially
			if auth.ProviderId == EveryonePseudoGroup {
				groupUUID = EveryonePseudoGroupUUID
			} else {
				// Resolve group name to internal_uuid from groups table
				// Use Provider field (was Idp)
				resolvedUUID, err := s.resolveGroupToUUID(tx, auth.ProviderId, &auth.Provider)
				if err != nil {
					slogging.Get().Debug("Could not resolve group identifier %s to internal_uuid: %v", auth.ProviderId, err)
					// Create group entry if it doesn't exist
					groupUUID, err = s.ensureGroupExists(tx, auth.ProviderId, &auth.Provider)
					if err != nil {
						return fmt.Errorf("failed to ensure group exists: %w", err)
					}
				} else {
					groupUUID = resolvedUUID
				}
			}
			userUUID = nil
		}

		// Insert or update authorization with dual FKs
		// Use different queries based on subject_type to handle different unique constraints
		var query string
		if subjectTypeStr == "user" {
			query = `
				INSERT INTO threat_model_access (
					threat_model_id, user_internal_uuid, group_internal_uuid,
					subject_type, role, created_at, modified_at
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (threat_model_id, user_internal_uuid, subject_type)
				DO UPDATE SET role = EXCLUDED.role, modified_at = EXCLUDED.modified_at`
		} else {
			query = `
				INSERT INTO threat_model_access (
					threat_model_id, user_internal_uuid, group_internal_uuid,
					subject_type, role, created_at, modified_at
				)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (threat_model_id, group_internal_uuid, subject_type)
				DO UPDATE SET role = EXCLUDED.role, modified_at = EXCLUDED.modified_at`
		}

		now := time.Now().UTC()
		slogging.Get().Debug("[DB-STORE] saveAuthorizationTx: Inserting entry - threat_model_id=%s, user_uuid=%v, group_uuid=%v, subject_type=%s, role=%s",
			threatModelId, userUUID, groupUUID, subjectTypeStr, auth.Role)
		_, err := tx.Exec(query, threatModelId, userUUID, groupUUID, subjectTypeStr, string(auth.Role), now, now)
		if err != nil {
			slogging.Get().Error("[DB-STORE] saveAuthorizationTx: Failed to insert authorization entry: %v", err)
			return err
		}
		slogging.Get().Debug("[DB-STORE] saveAuthorizationTx: Successfully inserted authorization entry")
	}

	slogging.Get().Debug("[DB-STORE] saveAuthorizationTx: Successfully saved all %d authorization entries", len(authorization))
	return nil
}

func (s *ThreatModelDatabaseStore) saveMetadataTx(tx *sql.Tx, threatModelId string, metadata []Metadata) error {
	if len(metadata) == 0 {
		return nil
	}

	for _, meta := range metadata {
		query := `
			INSERT INTO metadata (id, entity_type, entity_id, key, value, created_at, modified_at)
			VALUES ($1, 'threat_model', $2, $3, $4, $5, $6)
			ON CONFLICT (entity_type, entity_id, key)
			DO UPDATE SET value = EXCLUDED.value, modified_at = EXCLUDED.modified_at`

		now := time.Now().UTC()
		id := uuidgen.MustNewForEntity(uuidgen.EntityTypeMetadata)
		_, err := tx.Exec(query, id, threatModelId, meta.Key, meta.Value, now, now)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *ThreatModelDatabaseStore) updateAuthorizationTx(tx *sql.Tx, threatModelId string, authorization []Authorization) error {
	slogging.Get().Debug("[DB-STORE] updateAuthorizationTx: Deleting existing authorization for threat model %s", threatModelId)
	// Delete existing authorization
	result, err := tx.Exec(`DELETE FROM threat_model_access WHERE threat_model_id = $1`, threatModelId)
	if err != nil {
		return err
	}
	rowsDeleted, _ := result.RowsAffected()
	slogging.Get().Debug("[DB-STORE] updateAuthorizationTx: Deleted %d existing authorization entries", rowsDeleted)

	// Insert new authorization
	slogging.Get().Debug("[DB-STORE] updateAuthorizationTx: Inserting %d new authorization entries", len(authorization))
	return s.saveAuthorizationTx(tx, threatModelId, authorization)
}

func (s *ThreatModelDatabaseStore) updateMetadataTx(tx *sql.Tx, threatModelId string, metadata []Metadata) error {
	// Delete existing metadata
	_, err := tx.Exec(`DELETE FROM metadata WHERE entity_type = 'threat_model' AND entity_id = $1`, threatModelId)
	if err != nil {
		return err
	}

	// Insert new metadata
	return s.saveMetadataTx(tx, threatModelId, metadata)
}

// DiagramDatabaseStore handles diagram database operations
type DiagramDatabaseStore struct {
	db    *sql.DB
	mutex sync.RWMutex
}

// NewDiagramDatabaseStore creates a new diagram database store
func NewDiagramDatabaseStore(database *sql.DB) *DiagramDatabaseStore {
	return &DiagramDatabaseStore{
		db: database,
	}
}

// Get retrieves a diagram by ID
func (s *DiagramDatabaseStore) Get(id string) (DfdDiagram, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var diagram DfdDiagram
	var diagramUuid uuid.UUID
	var threatModelId uuid.UUID
	var name, diagramType string
	var cellsJSON []byte
	var createdAt, modifiedAt time.Time
	var updateVector int64
	var imageUpdateVector *int64

	var svgImageBytes []byte

	query := `
		SELECT id, threat_model_id, name, type, cells, svg_image, image_update_vector, update_vector, created_at, modified_at
		FROM diagrams 
		WHERE id = $1`

	err := s.db.QueryRow(query, id).Scan(
		&diagramUuid, &threatModelId, &name, &diagramType,
		&cellsJSON, &svgImageBytes, &imageUpdateVector, &updateVector, &createdAt, &modifiedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return diagram, fmt.Errorf("diagram with ID %s not found", id)
		}
		return diagram, fmt.Errorf("failed to get diagram: %w", err)
	}

	// Parse cells JSON
	var cells []DfdDiagram_Cells_Item
	if cellsJSON != nil {
		if err := json.Unmarshal(cellsJSON, &cells); err != nil {
			return diagram, fmt.Errorf("failed to parse cells JSON: %w", err)
		}
	}

	// Load diagram metadata from separate metadata table
	metadata, err := s.loadMetadata("diagram", diagramUuid.String())
	if err != nil {
		return diagram, fmt.Errorf("failed to load diagram metadata: %w", err)
	}

	// Convert type to enum
	diagType := DfdDiagramTypeDFD100 // default
	if diagramType != "" {
		diagType = DfdDiagramType(diagramType)
	}

	// Handle image - create struct with Svg and UpdateVector
	var imagePtr *struct {
		Svg          *[]byte `json:"svg,omitempty"`
		UpdateVector *int64  `json:"update_vector,omitempty"`
	}
	if svgImageBytes != nil {
		imagePtr = &struct {
			Svg          *[]byte `json:"svg,omitempty"`
			UpdateVector *int64  `json:"update_vector,omitempty"`
		}{
			Svg:          &svgImageBytes,
			UpdateVector: imageUpdateVector,
		}
	}

	diagram = DfdDiagram{
		Id:           &diagramUuid,
		Name:         name,
		Type:         diagType,
		Cells:        cells,
		Metadata:     &metadata,
		Image:        imagePtr,
		UpdateVector: &updateVector,
		CreatedAt:    &createdAt,
		ModifiedAt:   &modifiedAt,
	}

	// Store threat model ID in context for later use
	// This is a workaround since DfdDiagram doesn't have ThreatModelId field
	// We'll add it to metadata or handle it in the handler

	return diagram, nil
}

// List returns all diagrams (not used in current implementation)
func (s *DiagramDatabaseStore) List(offset, limit int, filter func(DfdDiagram) bool) []DfdDiagram {
	// Not implemented for diagrams as they're accessed through threat models
	return []DfdDiagram{}
}

// CreateWithThreatModel adds a new diagram with a specific threat model ID
func (s *DiagramDatabaseStore) CreateWithThreatModel(item DfdDiagram, threatModelID string, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Generate ID if not set
	id := uuid.New()
	if idSetter != nil {
		item = idSetter(item, id.String())
	}

	// Serialize cells to JSON
	cellsJSON, err := json.Marshal(item.Cells)
	if err != nil {
		return item, fmt.Errorf("failed to marshal cells: %w", err)
	}

	// Parse the threat model ID
	threatModelUUID, err := uuid.Parse(threatModelID)
	if err != nil {
		return item, fmt.Errorf("invalid threat model ID format: %w", err)
	}

	// Handle image - extract Svg field from the struct
	var svgImageBytes []byte
	var imageUpdateVector *int64
	if item.Image != nil && item.Image.Svg != nil {
		svgImageBytes = *item.Image.Svg
		imageUpdateVector = item.Image.UpdateVector
	}

	// Get update_vector (default to 0 for new diagrams)
	updateVector := int64(0)
	if item.UpdateVector != nil {
		updateVector = *item.UpdateVector
	}

	query := `
		INSERT INTO diagrams (id, threat_model_id, name, type, cells, svg_image, image_update_vector, update_vector, created_at, modified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err = s.db.Exec(query,
		id, threatModelUUID, item.Name, string(item.Type),
		cellsJSON, svgImageBytes, imageUpdateVector, updateVector, item.CreatedAt, item.ModifiedAt,
	)
	if err != nil {
		return item, fmt.Errorf("failed to insert diagram: %w", err)
	}

	return item, nil
}

// Create adds a new diagram (maintains backward compatibility)
func (s *DiagramDatabaseStore) Create(item DfdDiagram, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error) {
	// This method uses uuid.Nil for backward compatibility
	// New code should use CreateWithThreatModel instead
	return s.CreateWithThreatModel(item, uuid.Nil.String(), idSetter)
}

// Update modifies an existing diagram
func (s *DiagramDatabaseStore) Update(id string, item DfdDiagram) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Serialize cells to JSON
	cellsJSON, err := json.Marshal(item.Cells)
	if err != nil {
		return fmt.Errorf("failed to marshal cells: %w", err)
	}

	// Handle image - extract Svg field from the struct
	var svgImageBytes []byte
	var imageUpdateVector *int64
	if item.Image != nil && item.Image.Svg != nil {
		svgImageBytes = *item.Image.Svg
		imageUpdateVector = item.Image.UpdateVector
	}

	// Get update_vector (should be provided by caller)
	updateVector := int64(0)
	if item.UpdateVector != nil {
		updateVector = *item.UpdateVector
	}

	query := `
		UPDATE diagrams 
		SET name = $2, type = $3, cells = $4, svg_image = $5, image_update_vector = $6, update_vector = $7, modified_at = $8
		WHERE id = $1`

	result, err := s.db.Exec(query,
		id, item.Name, string(item.Type),
		cellsJSON, svgImageBytes, imageUpdateVector, updateVector, item.ModifiedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update diagram: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("diagram with ID %s not found", id)
	}

	return nil
}

// Delete removes a diagram
func (s *DiagramDatabaseStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `DELETE FROM diagrams WHERE id = $1`
	result, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete diagram: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("diagram with ID %s not found", id)
	}

	return nil
}

// Count returns the total number of diagrams
func (s *DiagramDatabaseStore) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int
	query := `SELECT COUNT(*) FROM diagrams`
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// loadMetadata loads metadata for a diagram from the metadata table
func (s *DiagramDatabaseStore) loadMetadata(entityType, entityID string) ([]Metadata, error) {
	query := `
		SELECT key, value 
		FROM metadata 
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY key ASC
	`

	rows, err := s.db.Query(query, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Error closing rows, but don't fail the operation
			_ = err
		}
	}()

	var metadata []Metadata
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		metadata = append(metadata, Metadata{
			Key:   key,
			Value: value,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating metadata: %w", err)
	}

	return metadata, nil
}

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
	"github.com/lib/pq"
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

	var tm ThreatModel
	var tmUUID uuid.UUID
	var name, ownerEmail, createdBy string
	var description, issueUrl *string
	var threatModelFramework string
	var status []string
	var statusUpdated *time.Time
	var createdAt, modifiedAt time.Time

	query := `
		SELECT id, name, description, owner_email, created_by,
		       threat_model_framework, issue_uri, status, status_updated,
		       created_at, modified_at
		FROM threat_models
		WHERE id = $1`

	slogging.Get().GetSlogger().Debug("Executing query", "query", query)
	slogging.Get().GetSlogger().Debug("Query parameter", "id", id, "type", fmt.Sprintf("%T", id), "length", len(id))

	// Try to validate the UUID format first
	if _, err := uuid.Parse(id); err != nil {
		slogging.Get().GetSlogger().Error("Invalid UUID format", "id", id, "error", err)
		return tm, fmt.Errorf("invalid UUID format: %w", err)
	}
	slogging.Get().GetSlogger().Debug("UUID format validation passed", "id", id)

	err := s.db.QueryRow(query, id).Scan(
		&tmUUID, &name, &description, &ownerEmail, &createdBy,
		&threatModelFramework, &issueUrl, pq.Array(&status), &statusUpdated,
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

	slogging.Get().GetSlogger().Debug("Query successful! Retrieved threat model", "id", tmUUID.String(), "name", name, "owner", ownerEmail)

	// Load authorization
	authorization, err := s.loadAuthorization(id)
	if err != nil {
		return tm, fmt.Errorf("failed to load authorization: %w", err)
	}

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
		Owner:                ownerEmail,
		CreatedBy:            &createdBy,
		ThreatModelFramework: framework,
		IssueUri:             issueUrl,
		Status:               &status,
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
		SELECT id, name, description, owner_email, created_by,
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
		var name, ownerEmail, createdBy string
		var description, issueUrl *string
		var threatModelFramework string
		var status []string
		var statusUpdated *time.Time
		var createdAt, modifiedAt time.Time

		err := rows.Scan(
			&uuid, &name, &description, &ownerEmail, &createdBy,
			&threatModelFramework, &issueUrl, pq.Array(&status), &statusUpdated,
			&createdAt, &modifiedAt,
		)
		if err != nil {
			continue
		}

		// Load authorization for filtering
		authorization, err := s.loadAuthorization(uuid.String())
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
			Owner:                ownerEmail,
			CreatedBy:            &createdBy,
			ThreatModelFramework: framework,
			IssueUri:             issueUrl,
			Status:               &status,
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
		SELECT id, name, description, owner_email, created_by,
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
		var name, ownerEmail, createdBy string
		var description, issueUrl *string
		var threatModelFramework string
		var status []string
		var statusUpdated *time.Time
		var createdAt, modifiedAt time.Time

		err := rows.Scan(
			&uuid, &name, &description, &ownerEmail, &createdBy,
			&threatModelFramework, &issueUrl, pq.Array(&status), &statusUpdated,
			&createdAt, &modifiedAt,
		)
		if err != nil {
			continue
		}

		// Load authorization for filtering
		authorization, err := s.loadAuthorization(uuid.String())
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
			Owner:                ownerEmail,
			CreatedBy:            &createdBy,
			ThreatModelFramework: framework,
			IssueUri:             issueUrl,
			Status:               &status,
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
			actualSourceCount := s.calculateSourceCount(uuid.String())
			actualNoteCount := s.calculateNoteCount(uuid.String())
			actualAssetCount := s.calculateAssetCount(uuid.String())

			results = append(results, ThreatModelWithCounts{
				ThreatModel:   tm,
				DocumentCount: actualDocumentCount,
				SourceCount:   actualSourceCount,
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

// calculateSourceCount counts the actual number of sources for a threat model
func (s *ThreatModelDatabaseStore) calculateSourceCount(threatModelId string) int {
	query := `SELECT COUNT(*) FROM sources WHERE threat_model_id = $1`
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

	// Insert threat model
	query := `
		INSERT INTO threat_models (id, name, description, owner_email, created_by,
		                          threat_model_framework, issue_uri, status, status_updated,
		                          created_at, modified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err = tx.Exec(query,
		id, item.Name, item.Description, item.Owner, item.CreatedBy,
		framework, item.IssueUri, pq.Array(item.Status), statusUpdated,
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
	var oldStatus []string
	err = tx.QueryRow("SELECT status FROM threat_models WHERE id = $1", id).Scan(pq.Array(&oldStatus))
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get current status: %w", err)
	}

	// Determine if status changed
	statusChanged := false
	newStatus := item.Status
	if newStatus == nil && oldStatus != nil {
		statusChanged = true
	} else if newStatus != nil && oldStatus == nil {
		statusChanged = true
	} else if newStatus != nil && oldStatus != nil {
		if len(*newStatus) != len(oldStatus) {
			statusChanged = true
		} else {
			for i := range *newStatus {
				if (*newStatus)[i] != oldStatus[i] {
					statusChanged = true
					break
				}
			}
		}
	}

	// Set status_updated if status changed
	var statusUpdated *time.Time
	if statusChanged {
		now := time.Now().UTC()
		statusUpdated = &now
	}

	// Update threat model
	query := `
		UPDATE threat_models
		SET name = $2, description = $3, owner_email = $4, created_by = $5,
		    threat_model_framework = $6, issue_uri = $7, status = $8, status_updated = $9,
		    modified_at = $10
		WHERE id = $1`

	result, err := tx.Exec(query,
		id, item.Name, item.Description, item.Owner, item.CreatedBy,
		framework, item.IssueUri, pq.Array(item.Status), statusUpdated,
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

func (s *ThreatModelDatabaseStore) loadAuthorization(threatModelId string) ([]Authorization, error) {
	query := `
		SELECT subject, subject_type, idp, role
		FROM threat_model_access
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

	var authorization []Authorization
	for rows.Next() {
		var subject, subjectTypeStr, roleStr string
		var idp sql.NullString
		if err := rows.Scan(&subject, &subjectTypeStr, &idp, &roleStr); err != nil {
			continue
		}

		role := AuthorizationRole(roleStr)

		// Convert string subject_type to proper enum
		var subjectType AuthorizationSubjectType
		switch subjectTypeStr {
		case "user":
			subjectType = AuthorizationSubjectTypeUser
		case "group":
			subjectType = AuthorizationSubjectTypeGroup
		default:
			// For backward compatibility, treat empty or unknown as user
			subjectType = AuthorizationSubjectTypeUser
		}

		auth := Authorization{
			Subject:     subject,
			SubjectType: subjectType,
			Role:        role,
		}

		// Set IdP if present
		if idp.Valid && idp.String != "" {
			auth.Idp = &idp.String
		}

		authorization = append(authorization, auth)
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

	var threats []Threat
	for rows.Next() {
		var id, threatModelUuid uuid.UUID
		var name, priority, status, threatType string
		var description, severityStr, mitigation *string
		var diagramIdStr, cellIdStr, assetIdStr *string
		var issueUrl *string
		var score *float64
		var mitigated bool
		var createdAt, modifiedAt time.Time

		if err := rows.Scan(&id, &name, &description, &severityStr, &mitigation, &diagramIdStr, &cellIdStr, &assetIdStr,
			&priority, &mitigated, &status, &threatType, &score, &issueUrl,
			&createdAt, &modifiedAt); err != nil {
			continue
		}

		// Convert threat model ID
		threatModelUuid, _ = uuid.Parse(threatModelId)

		// Convert severity
		severity := ThreatSeverityUnknown // default
		if severityStr != nil && *severityStr != "" {
			severity = ThreatSeverity(*severityStr)
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
		return nil, nil // No diagrams
	}

	// Load each diagram from the DiagramStore to ensure single source of truth
	var diagrams []Diagram
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
		if auth.SubjectType == AuthorizationSubjectTypeGroup {
			subjectTypeStr = "group"
		}

		// Handle nullable idp field
		var idpValue interface{}
		if auth.Idp != nil && *auth.Idp != "" {
			idpValue = *auth.Idp
		} else {
			idpValue = nil
		}

		query := `
			INSERT INTO threat_model_access (threat_model_id, subject, subject_type, idp, role, created_at, modified_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (threat_model_id, subject, subject_type, idp)
			DO UPDATE SET role = $5, modified_at = $7`

		now := time.Now().UTC()
		_, err := tx.Exec(query, threatModelId, auth.Subject, subjectTypeStr, idpValue, string(auth.Role), now, now)
		if err != nil {
			return err
		}
	}

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
	// Delete existing authorization
	_, err := tx.Exec(`DELETE FROM threat_model_access WHERE threat_model_id = $1`, threatModelId)
	if err != nil {
		return err
	}

	// Insert new authorization
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
		CreatedAt:    createdAt,
		ModifiedAt:   modifiedAt,
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

// loadDiagramMetadata loads metadata for a diagram from the metadata table
func (s *ThreatModelDatabaseStore) loadDiagramMetadata(diagramID string) ([]Metadata, error) {
	query := `
		SELECT key, value 
		FROM metadata 
		WHERE entity_type = 'diagram' AND entity_id = $1
		ORDER BY key ASC
	`

	rows, err := s.db.Query(query, diagramID)
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

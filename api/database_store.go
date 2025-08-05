package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v4/stdlib"
)

// DatabaseStore provides a database-backed store implementation
type DatabaseStore[T any] struct {
	db         *sql.DB
	mutex      sync.RWMutex
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

	var tm ThreatModel
	var uuid uuid.UUID
	var name, ownerEmail, createdBy string
	var description, issueUrl *string
	var threatModelFramework string
	var createdAt, updatedAt time.Time

	query := `
		SELECT id, name, description, owner_email, created_by, 
		       threat_model_framework, issue_url, created_at, updated_at,
		       document_count, source_count, diagram_count, threat_count
		FROM threat_models 
		WHERE id = $1`

	var documentCount, sourceCount, diagramCount, threatCount int
	err := s.db.QueryRow(query, id).Scan(
		&uuid, &name, &description, &ownerEmail, &createdBy,
		&threatModelFramework, &issueUrl, &createdAt, &updatedAt,
		&documentCount, &sourceCount, &diagramCount, &threatCount,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return tm, fmt.Errorf("threat model with ID %s not found", id)
		}
		return tm, fmt.Errorf("failed to get threat model: %w", err)
	}

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

	// Load diagrams
	diagrams, err := s.loadDiagrams(id)
	if err != nil {
		return tm, fmt.Errorf("failed to load diagrams: %w", err)
	}

	// Convert framework to enum
	framework := ThreatModelThreatModelFramework(threatModelFramework)
	if threatModelFramework == "" {
		framework = ThreatModelThreatModelFrameworkSTRIDE // default
	}

	tm = ThreatModel{
		Id:                   &uuid,
		Name:                 name,
		Description:          description,
		Owner:                ownerEmail,
		CreatedBy:            createdBy,
		ThreatModelFramework: framework,
		IssueUrl:             issueUrl,
		CreatedAt:            createdAt,
		ModifiedAt:           updatedAt,
		Authorization:        authorization,
		Metadata:             &metadata,
		Threats:              &threats,
		Diagrams:             &diagrams,
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
		       threat_model_framework, issue_url, created_at, updated_at,
		       document_count, source_count, diagram_count, threat_count
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
		var createdAt, updatedAt time.Time
		var documentCount, sourceCount, diagramCount, threatCount int

		err := rows.Scan(
			&uuid, &name, &description, &ownerEmail, &createdBy,
			&threatModelFramework, &issueUrl, &createdAt, &updatedAt,
			&documentCount, &sourceCount, &diagramCount, &threatCount,
		)
		if err != nil {
			continue
		}

		// Load authorization for filtering
		authorization, err := s.loadAuthorization(uuid.String())
		if err != nil {
			continue
		}

		// Convert framework to enum
		framework := ThreatModelThreatModelFramework(threatModelFramework)
		if threatModelFramework == "" {
			framework = ThreatModelThreatModelFrameworkSTRIDE // default
		}

		tm = ThreatModel{
			Id:                   &uuid,
			Name:                 name,
			Description:          description,
			Owner:                ownerEmail,
			CreatedBy:            createdBy,
			ThreatModelFramework: framework,
			IssueUrl:             issueUrl,
			CreatedAt:            createdAt,
			ModifiedAt:           updatedAt,
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
		       threat_model_framework, issue_url, created_at, updated_at,
		       document_count, source_count, diagram_count, threat_count
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
		var createdAt, updatedAt time.Time
		var documentCount, sourceCount, diagramCount, threatCount int

		err := rows.Scan(
			&uuid, &name, &description, &ownerEmail, &createdBy,
			&threatModelFramework, &issueUrl, &createdAt, &updatedAt,
			&documentCount, &sourceCount, &diagramCount, &threatCount,
		)
		if err != nil {
			continue
		}

		// Load authorization for filtering
		authorization, err := s.loadAuthorization(uuid.String())
		if err != nil {
			continue
		}

		// Convert framework to enum
		framework := ThreatModelThreatModelFramework(threatModelFramework)
		if threatModelFramework == "" {
			framework = ThreatModelThreatModelFrameworkSTRIDE // default
		}

		tm = ThreatModel{
			Id:                   &uuid,
			Name:                 name,
			Description:          description,
			Owner:                ownerEmail,
			CreatedBy:            createdBy,
			ThreatModelFramework: framework,
			IssueUrl:             issueUrl,
			CreatedAt:            createdAt,
			ModifiedAt:           updatedAt,
			Authorization:        authorization,
		}

		// Apply filter if provided
		if filter == nil || filter(tm) {
			results = append(results, ThreatModelWithCounts{
				ThreatModel:   tm,
				DocumentCount: documentCount,
				SourceCount:   sourceCount,
				DiagramCount:  diagramCount,
				ThreatCount:   threatCount,
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
	framework := string(item.ThreatModelFramework)
	if framework == "" {
		framework = "STRIDE" // default
	}

	// Insert threat model
	query := `
		INSERT INTO threat_models (id, name, description, owner_email, created_by, 
		                          threat_model_framework, issue_url, created_at, updated_at,
		                          document_count, source_count, diagram_count, threat_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

	_, err = tx.Exec(query,
		id, item.Name, item.Description, item.Owner, item.CreatedBy,
		framework, item.IssueUrl, item.CreatedAt, item.ModifiedAt,
		0, 0, 0, 0, // Initialize all counts to 0
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
	framework := string(item.ThreatModelFramework)
	if framework == "" {
		framework = "STRIDE" // default
	}

	// Update threat model
	query := `
		UPDATE threat_models 
		SET name = $2, description = $3, owner_email = $4, created_by = $5,
		    threat_model_framework = $6, issue_url = $7, updated_at = $8
		WHERE id = $1`

	result, err := tx.Exec(query,
		id, item.Name, item.Description, item.Owner, item.CreatedBy,
		framework, item.IssueUrl, item.ModifiedAt,
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

// UpdateCounts recomputes and updates all count fields for a threat model
func (s *ThreatModelDatabaseStore) UpdateCounts(threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Compute counts from related tables
	documentCount, err := s.countDocuments(threatModelID)
	if err != nil {
		return fmt.Errorf("failed to count documents: %w", err)
	}

	sourceCount, err := s.countSources(threatModelID)
	if err != nil {
		return fmt.Errorf("failed to count sources: %w", err)
	}

	diagramCount, err := s.countDiagrams(threatModelID)
	if err != nil {
		return fmt.Errorf("failed to count diagrams: %w", err)
	}

	threatCount, err := s.countThreats(threatModelID)
	if err != nil {
		return fmt.Errorf("failed to count threats: %w", err)
	}

	// Update the counts in the threat_models table
	return s.updateCountFields(threatModelID, documentCount, sourceCount, diagramCount, threatCount)
}

// UpdateCountsWithValues updates count fields with specific provided values (for PUT operations)
func (s *ThreatModelDatabaseStore) UpdateCountsWithValues(threatModelID string, documentCount, sourceCount, diagramCount, threatCount int) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.updateCountFields(threatModelID, documentCount, sourceCount, diagramCount, threatCount)
}

// CountSubEntitiesFromPayload counts entities from a threat model payload during PUT operations
func (s *ThreatModelDatabaseStore) CountSubEntitiesFromPayload(tm ThreatModel) (documentCount, sourceCount, diagramCount, threatCount int) {
	if tm.Documents != nil {
		documentCount = len(*tm.Documents)
	}
	if tm.SourceCode != nil {
		sourceCount = len(*tm.SourceCode)
	}
	if tm.Diagrams != nil {
		diagramCount = len(*tm.Diagrams)
	}
	if tm.Threats != nil {
		threatCount = len(*tm.Threats)
	}
	return
}

// updateCountFields updates the count fields in the threat_models table with constraint violation handling
func (s *ThreatModelDatabaseStore) updateCountFields(threatModelID string, documentCount, sourceCount, diagramCount, threatCount int) error {
	query := `
		UPDATE threat_models 
		SET document_count = $2, source_count = $3, diagram_count = $4, threat_count = $5
		WHERE id = $1`

	_, err := s.db.Exec(query, threatModelID, documentCount, sourceCount, diagramCount, threatCount)
	if err != nil {
		// Check if this is a constraint violation
		if isConstraintViolation(err) {
			// Log the constraint violation but don't fail the operation
			fmt.Printf("[ERROR] Count constraint violation for threat model %s: doc=%d, src=%d, diag=%d, threat=%d - %v\n",
				threatModelID, documentCount, sourceCount, diagramCount, threatCount, err)
		}
		return fmt.Errorf("failed to update count fields: %w", err)
	}

	return nil
}

// Helper functions to count related entities
func (s *ThreatModelDatabaseStore) countDocuments(threatModelID string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM documents WHERE threat_model_id = $1`
	err := s.db.QueryRow(query, threatModelID).Scan(&count)
	return count, err
}

func (s *ThreatModelDatabaseStore) countSources(threatModelID string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM sources WHERE threat_model_id = $1`
	err := s.db.QueryRow(query, threatModelID).Scan(&count)
	return count, err
}

func (s *ThreatModelDatabaseStore) countDiagrams(threatModelID string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM diagrams WHERE threat_model_id = $1`
	err := s.db.QueryRow(query, threatModelID).Scan(&count)
	return count, err
}

func (s *ThreatModelDatabaseStore) countThreats(threatModelID string) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM threats WHERE threat_model_id = $1`
	err := s.db.QueryRow(query, threatModelID).Scan(&count)
	return count, err
}

// isConstraintViolation checks if an error is a PostgreSQL constraint violation
func isConstraintViolation(err error) bool {
	if err == nil {
		return false
	}
	// Check for PostgreSQL constraint violation error codes
	errStr := err.Error()
	return containsAny(errStr, []string{
		"check constraint",
		"constraint violation",
		"violates check constraint",
	})
}

// containsAny checks if a string contains any of the given substrings
func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

// Helper methods for loading related data

func (s *ThreatModelDatabaseStore) loadAuthorization(threatModelId string) ([]Authorization, error) {
	query := `
		SELECT user_email, role 
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
		var userEmail, roleStr string
		if err := rows.Scan(&userEmail, &roleStr); err != nil {
			continue
		}

		role := AuthorizationRole(roleStr)
		authorization = append(authorization, Authorization{
			Subject: userEmail,
			Role:    role,
		})
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

func (s *ThreatModelDatabaseStore) loadThreats(threatModelId string) ([]Threat, error) {
	query := `
		SELECT id, name, description, severity, mitigation, created_at, updated_at
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
		var name string
		var description, severityStr, mitigation *string
		var createdAt, modifiedAt time.Time

		if err := rows.Scan(&id, &name, &description, &severityStr, &mitigation, &createdAt, &modifiedAt); err != nil {
			continue
		}

		// Convert threat model ID
		threatModelUuid, _ = uuid.Parse(threatModelId)

		// Convert severity
		severity := Unknown // default
		if severityStr != nil && *severityStr != "" {
			severity = ThreatSeverity(*severityStr)
		}

		threats = append(threats, Threat{
			Id:            &id,
			Name:          name,
			Description:   description,
			Severity:      severity,
			Mitigation:    mitigation,
			CreatedAt:     createdAt,
			ModifiedAt:    modifiedAt,
			ThreatModelId: threatModelUuid,
			Priority:      "Medium", // default
			Status:        "Open",   // default
			ThreatType:    "",       // default
			Mitigated:     false,    // default
		})
	}

	return threats, nil
}

func (s *ThreatModelDatabaseStore) loadDiagrams(threatModelId string) ([]Diagram, error) {
	query := `
		SELECT id, name, type, content, cells, metadata, created_at, updated_at
		FROM diagrams 
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

	var diagrams []Diagram
	for rows.Next() {
		var diagramUuid uuid.UUID
		var name, diagramType string
		var description *string
		var cellsJSON, metadataJSON []byte
		var createdAt, updatedAt time.Time

		if err := rows.Scan(&diagramUuid, &name, &diagramType, &description, &cellsJSON, &metadataJSON, &createdAt, &updatedAt); err != nil {
			continue
		}

		// Parse cells JSON
		var cells []DfdDiagram_Cells_Item
		if cellsJSON != nil {
			if err := json.Unmarshal(cellsJSON, &cells); err != nil {
				continue // Skip this diagram if cells can't be parsed
			}
		}

		// Parse metadata JSON
		var metadata []Metadata
		if metadataJSON != nil {
			if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
				continue // Skip this diagram if metadata can't be parsed
			}
		}

		// Convert type to enum
		diagType := DfdDiagramTypeDFD100 // default
		if diagramType != "" {
			diagType = DfdDiagramType(diagramType)
		}

		// Create DfdDiagram
		dfdDiagram := DfdDiagram{
			Id:          &diagramUuid,
			Name:        name,
			Description: description,
			Type:        diagType,
			Cells:       cells,
			Metadata:    &metadata,
			CreatedAt:   createdAt,
			ModifiedAt:  updatedAt,
		}

		// Convert to Diagram union type
		var diagramUnion Diagram
		if err := diagramUnion.FromDfdDiagram(dfdDiagram); err != nil {
			continue // Skip this diagram if union conversion fails
		}

		diagrams = append(diagrams, diagramUnion)
	}

	return diagrams, nil
}

func (s *ThreatModelDatabaseStore) saveAuthorizationTx(tx *sql.Tx, threatModelId string, authorization []Authorization) error {
	if len(authorization) == 0 {
		return nil
	}

	for _, auth := range authorization {
		query := `
			INSERT INTO threat_model_access (threat_model_id, user_email, role, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (threat_model_id, user_email) 
			DO UPDATE SET role = $3, updated_at = $5`

		now := time.Now().UTC()
		_, err := tx.Exec(query, threatModelId, auth.Subject, string(auth.Role), now, now)
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
			INSERT INTO metadata (entity_type, entity_id, key, value, created_at, updated_at)
			VALUES ('threat_model', $1, $2, $3, $4, $5)
			ON CONFLICT (entity_type, entity_id, key)
			DO UPDATE SET value = $3, updated_at = $5`

		now := time.Now().UTC()
		_, err := tx.Exec(query, threatModelId, meta.Key, meta.Value, now, now)
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
	var description *string
	var cellsJSON, metadataJSON []byte
	var createdAt, updatedAt time.Time

	query := `
		SELECT id, threat_model_id, name, type, content, cells, metadata, created_at, updated_at
		FROM diagrams 
		WHERE id = $1`

	err := s.db.QueryRow(query, id).Scan(
		&diagramUuid, &threatModelId, &name, &diagramType, &description,
		&cellsJSON, &metadataJSON, &createdAt, &updatedAt,
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

	// Parse metadata JSON
	var metadata []Metadata
	if metadataJSON != nil {
		if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
			return diagram, fmt.Errorf("failed to parse metadata JSON: %w", err)
		}
	}

	// Convert type to enum
	diagType := DfdDiagramTypeDFD100 // default
	if diagramType != "" {
		diagType = DfdDiagramType(diagramType)
	}

	diagram = DfdDiagram{
		Id:          &diagramUuid,
		Name:        name,
		Description: description,
		Type:        diagType,
		Cells:       cells,
		Metadata:    &metadata,
		CreatedAt:   createdAt,
		ModifiedAt:  updatedAt,
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

	// Serialize metadata to JSON
	var metadataJSON []byte
	if item.Metadata != nil {
		metadataJSON, err = json.Marshal(*item.Metadata)
		if err != nil {
			return item, fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	// Parse the threat model ID
	threatModelUUID, err := uuid.Parse(threatModelID)
	if err != nil {
		return item, fmt.Errorf("invalid threat model ID format: %w", err)
	}

	query := `
		INSERT INTO diagrams (id, threat_model_id, name, type, content, cells, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err = s.db.Exec(query,
		id, threatModelUUID, item.Name, string(item.Type), item.Description,
		cellsJSON, metadataJSON, item.CreatedAt, item.ModifiedAt,
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

	// Serialize metadata to JSON
	var metadataJSON []byte
	if item.Metadata != nil {
		metadataJSON, err = json.Marshal(*item.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	query := `
		UPDATE diagrams 
		SET name = $2, type = $3, content = $4, cells = $5, metadata = $6, updated_at = $7
		WHERE id = $1`

	result, err := s.db.Exec(query,
		id, item.Name, string(item.Type), item.Description,
		cellsJSON, metadataJSON, item.ModifiedAt,
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

package auth

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
)

// GroupDeletionResult contains statistics about the group deletion operation
type GroupDeletionResult struct {
	ThreatModelsDeleted  int    `json:"threat_models_deleted"`
	ThreatModelsRetained int    `json:"threat_models_retained"`
	GroupName            string `json:"group_name"`
}

// DeleteGroupAndData deletes a TMI-managed group and handles threat model cleanup
// Groups are always provider-independent (provider="*") in TMI
func (s *Service) DeleteGroupAndData(ctx context.Context, groupName string) (*GroupDeletionResult, error) {
	logger := slogging.Get()
	db := s.dbManager.Postgres().GetDB()

	// Validate not deleting "everyone" group
	if groupName == "everyone" {
		return nil, fmt.Errorf("cannot delete protected group: everyone")
	}

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			logger.Error("Failed to rollback group deletion transaction: %v", err)
		}
	}()

	result := &GroupDeletionResult{
		GroupName: groupName,
	}

	// Get group internal_uuid (provider is always "*" for TMI-managed groups)
	var groupInternalUUID string
	err = tx.QueryRowContext(ctx,
		`SELECT internal_uuid FROM groups WHERE provider = '*' AND group_name = $1`,
		groupName).Scan(&groupInternalUUID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("group not found: %s", groupName)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query group: %w", err)
	}

	// Get all threat models owned by this group
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM threat_models WHERE owner_internal_uuid = $1`,
		groupInternalUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to query owned threat models: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			logger.Error("Failed to close rows: %v", cerr)
		}
	}()

	var threatModelIDs []string
	for rows.Next() {
		var tmID string
		if err := rows.Scan(&tmID); err != nil {
			return nil, fmt.Errorf("failed to scan threat model ID: %w", err)
		}
		threatModelIDs = append(threatModelIDs, tmID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating threat models: %w", err)
	}

	// Process each threat model
	for _, tmID := range threatModelIDs {
		// Check if there are other user owners (not group owners)
		var hasUserOwner bool
		err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM threat_model_access
				WHERE threat_model_id = $1
				  AND role = 'owner'
				  AND subject_type = 'user'
			)`,
			tmID).Scan(&hasUserOwner)

		if err != nil {
			return nil, fmt.Errorf("failed to check for alternate owners for threat model %s: %w", tmID, err)
		}

		if !hasUserOwner {
			// No user owners - delete threat model (cascades to children)
			_, err = tx.ExecContext(ctx,
				`DELETE FROM threat_models WHERE id = $1`,
				tmID)
			if err != nil {
				return nil, fmt.Errorf("failed to delete threat model %s: %w", tmID, err)
			}
			result.ThreatModelsDeleted++
			logger.Debug("Deleted threat model %s (no user owners)", tmID)
		} else {
			// Has user owners - just remove group from access, keep threat model
			result.ThreatModelsRetained++
			logger.Debug("Retaining threat model %s (has user owners)", tmID)
		}
	}

	// Clean up any remaining permissions (reader/writer/owner on any threat models)
	_, err = tx.ExecContext(ctx,
		`DELETE FROM threat_model_access WHERE group_internal_uuid = $1 AND subject_type = 'group'`,
		groupInternalUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to clean up group permissions: %w", err)
	}

	// Delete group record (cascades to administrators via FK)
	deleteResult, err := tx.ExecContext(ctx,
		`DELETE FROM groups WHERE internal_uuid = $1`,
		groupInternalUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to delete group: %w", err)
	}

	rowsAffected, err := deleteResult.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("group not found: %s", groupName)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	logger.Info("Group deleted successfully: group_name=%s, deleted=%d, retained=%d",
		groupName, result.ThreatModelsDeleted, result.ThreatModelsRetained)

	return result, nil
}

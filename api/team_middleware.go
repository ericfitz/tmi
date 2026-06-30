package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// teamAuthDB is the GORM database handle used for team authorization queries.
// Set during store initialization via InitializeGormStores.
var teamAuthDB *gorm.DB

// SetTeamAuthDB sets the GORM database handle for team authorization.
// SEM@8c7929da791c778ff88713684c47aa2a10911bba: register the GORM database handle for team authorization queries (mutates shared state)
func SetTeamAuthDB(db *gorm.DB) {
	teamAuthDB = db
}

// IsTeamMemberOrAdmin checks if a user is a member of the specified team OR an administrator.
// Returns true if the user is authorized to access the team's resources.
// SEM@c99517d0f78396ed3e7b16e756e0318aefc525db: authorize whether a user is a team member or administrator (reads DB)
func IsTeamMemberOrAdmin(ctx context.Context, teamID string, userInternalUUID string, c *gin.Context) (bool, error) {
	logger := slogging.Get()

	// Check administrator status first (fast path)
	if c != nil {
		isAdmin, err := IsUserAdministrator(c)
		if err != nil {
			logger.Debug("IsTeamMemberOrAdmin: admin check failed: %v", err)
		} else if isAdmin {
			return true, nil
		}
	}

	// Check team membership
	if teamAuthDB == nil {
		logger.Error("IsTeamMemberOrAdmin: teamAuthDB not initialized")
		return false, nil
	}

	var count int64
	result := teamAuthDB.WithContext(ctx).Model(&models.TeamMemberRecord{}).
		Where(ColumnMap(teamAuthDB.Name(), map[string]any{"team_id": teamID, "user_internal_uuid": userInternalUUID})).
		Count(&count)
	if result.Error != nil {
		logger.Error("IsTeamMemberOrAdmin: membership query failed: %v", result.Error)
		return false, result.Error
	}

	return count > 0, nil
}

// IsTeamOwnerOrAdmin checks if a user is the creator/owner of a team or an administrator.
// Used for operations that require owner-level access (e.g., delete).
// SEM@c99517d0f78396ed3e7b16e756e0318aefc525db: authorize whether a user is the team creator or administrator (reads DB)
func IsTeamOwnerOrAdmin(ctx context.Context, teamID string, userInternalUUID string, c *gin.Context) (bool, error) {
	logger := slogging.Get()

	// Check administrator status first (fast path)
	if c != nil {
		isAdmin, err := IsUserAdministrator(c)
		if err != nil {
			logger.Debug("IsTeamOwnerOrAdmin: admin check failed: %v", err)
		} else if isAdmin {
			return true, nil
		}
	}

	if teamAuthDB == nil {
		logger.Error("IsTeamOwnerOrAdmin: teamAuthDB not initialized")
		return false, nil
	}

	// Check if user is the team creator
	var team models.TeamRecord
	result := teamAuthDB.WithContext(ctx).
		Where(ColumnMap(teamAuthDB.Name(), map[string]any{"id": teamID})).
		First(&team)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return false, gorm.ErrRecordNotFound
		}
		logger.Error("IsTeamOwnerOrAdmin: team lookup failed: %v", result.Error)
		return false, result.Error
	}

	return string(team.CreatedByInternalUUID) == userInternalUUID, nil
}

// IsProjectTeamMemberOrAdmin checks if a user is a member of the team that owns the project, or an administrator.
// SEM@c99517d0f78396ed3e7b16e756e0318aefc525db: authorize whether a user belongs to the team that owns a project, or is an administrator (reads DB)
func IsProjectTeamMemberOrAdmin(ctx context.Context, projectID string, userInternalUUID string, c *gin.Context) (bool, error) {
	logger := slogging.Get()

	// Check administrator status first (fast path)
	if c != nil {
		isAdmin, err := IsUserAdministrator(c)
		if err != nil {
			logger.Debug("IsProjectTeamMemberOrAdmin: admin check failed: %v", err)
		} else if isAdmin {
			return true, nil
		}
	}

	if teamAuthDB == nil {
		logger.Error("IsProjectTeamMemberOrAdmin: teamAuthDB not initialized")
		return false, nil
	}

	// Look up the project's team_id
	var project models.ProjectRecord
	result := teamAuthDB.WithContext(ctx).
		Where(ColumnMap(teamAuthDB.Name(), map[string]any{"id": projectID})).
		First(&project)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return false, gorm.ErrRecordNotFound
		}
		logger.Error("IsProjectTeamMemberOrAdmin: project lookup failed: %v", result.Error)
		return false, result.Error
	}

	// Check team membership
	return IsTeamMemberOrAdmin(ctx, string(project.TeamID), userInternalUUID, c)
}

// GetProjectTeamID retrieves the team_id for a given project.
// SEM@c99517d0f78396ed3e7b16e756e0318aefc525db: fetch the team ID that owns a given project (reads DB)
func GetProjectTeamID(ctx context.Context, projectID string) (string, error) {
	if teamAuthDB == nil {
		return "", fmt.Errorf("database not initialized") //nolint:goerr113
	}

	var project models.ProjectRecord
	result := teamAuthDB.WithContext(ctx).
		Where(ColumnMap(teamAuthDB.Name(), map[string]any{"id": projectID})).
		Select("team_id").
		First(&project)
	if result.Error != nil {
		return "", result.Error
	}

	return string(project.TeamID), nil
}

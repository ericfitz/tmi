package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// MaxPreferencesSize is the maximum size in bytes for user preferences JSON
	MaxPreferencesSize = 1024 // 1KB

	// MaxPreferencesClients is the maximum number of client entries allowed
	MaxPreferencesClients = 20
)

// clientKeyPattern validates client identifiers (1-64 chars, alphanumeric + underscore/hyphen)
var clientKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// validatePreferences validates the preferences JSON data
func validatePreferences(data []byte) error {
	if len(data) > MaxPreferencesSize {
		return InvalidInputError(fmt.Sprintf("preferences exceed 1KB limit (%d bytes)", len(data)))
	}

	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return InvalidInputError(fmt.Sprintf("invalid JSON: %v", err))
	}

	if len(prefs) > MaxPreferencesClients {
		return InvalidInputError(fmt.Sprintf("maximum %d client entries allowed, got %d", MaxPreferencesClients, len(prefs)))
	}

	for key := range prefs {
		if !clientKeyPattern.MatchString(key) {
			return InvalidInputError(fmt.Sprintf("invalid client key '%s': must be 1-64 alphanumeric characters, underscores, or hyphens", key))
		}
	}

	return nil
}

// GetCurrentUserPreferences handles GET /me/preferences
func (s *Server) GetCurrentUserPreferences(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	logger.Info("[PREFERENCES] GetCurrentUserPreferences called")

	userUUID := c.GetString("userInternalUUID")
	if userUUID == "" {
		logger.Warn("[PREFERENCES] No user UUID in context")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "user not authenticated",
		})
		return
	}

	// Get database from ThreatModelStore
	dbStore, ok := ThreatModelStore.(*GormThreatModelStore)
	if !ok {
		logger.Error("[PREFERENCES] ThreatModelStore is not a database store")
		HandleRequestError(c, ServerError("database not available"))
		return
	}
	db := dbStore.GetDB()

	var pref models.UserPreference
	result := db.WithContext(c.Request.Context()).
		Where("user_internal_uuid = ?", userUUID).
		First(&pref)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			// Return empty object if no preferences exist
			c.JSON(http.StatusOK, UserPreferences{})
			return
		}
		logger.Error("[PREFERENCES] Failed to get preferences: %v", result.Error)
		HandleRequestError(c, ServerError("failed to retrieve preferences"))
		return
	}

	// Parse the stored JSON into the response type
	var prefs UserPreferences
	if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
		logger.Error("[PREFERENCES] Failed to unmarshal preferences: %v", err)
		HandleRequestError(c, ServerError("failed to parse preferences"))
		return
	}

	c.JSON(http.StatusOK, prefs)
}

// CreateCurrentUserPreferences handles POST /me/preferences
func (s *Server) CreateCurrentUserPreferences(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	logger.Info("[PREFERENCES] CreateCurrentUserPreferences called")

	userUUID := c.GetString("userInternalUUID")
	if userUUID == "" {
		logger.Warn("[PREFERENCES] No user UUID in context")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "user not authenticated",
		})
		return
	}

	// Read request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Error("[PREFERENCES] Failed to read request body: %v", err)
		HandleRequestError(c, InvalidInputError("failed to read request body"))
		return
	}

	// Validate preferences
	if err := validatePreferences(body); err != nil {
		logger.Warn("[PREFERENCES] Validation failed: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Get database from ThreatModelStore
	dbStore, ok := ThreatModelStore.(*GormThreatModelStore)
	if !ok {
		logger.Error("[PREFERENCES] ThreatModelStore is not a database store")
		HandleRequestError(c, ServerError("database not available"))
		return
	}
	db := dbStore.GetDB()

	// Check if preferences already exist
	var existing models.UserPreference
	result := db.WithContext(c.Request.Context()).
		Where("user_internal_uuid = ?", userUUID).
		First(&existing)

	if result.Error == nil {
		// Preferences already exist
		logger.Warn("[PREFERENCES] Preferences already exist for user %s", userUUID)
		HandleRequestError(c, ConflictError("preferences already exist (use PUT to update)"))
		return
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		logger.Error("[PREFERENCES] Failed to check existing preferences: %v", result.Error)
		HandleRequestError(c, ServerError("failed to check existing preferences"))
		return
	}

	// Create new preferences
	now := time.Now()
	pref := models.UserPreference{
		ID:               uuid.New().String(),
		UserInternalUUID: userUUID,
		Preferences:      models.JSONRaw(body),
		CreatedAt:        now,
		ModifiedAt:       now,
	}

	if err := db.WithContext(c.Request.Context()).Create(&pref).Error; err != nil {
		logger.Error("[PREFERENCES] Failed to create preferences: %v", err)
		HandleRequestError(c, ServerError("failed to create preferences"))
		return
	}

	// Parse and return the preferences
	var prefs UserPreferences
	if err := json.Unmarshal(body, &prefs); err != nil {
		logger.Error("[PREFERENCES] Failed to unmarshal preferences for response: %v", err)
		HandleRequestError(c, ServerError("failed to parse preferences"))
		return
	}

	logger.Info("[PREFERENCES] Created preferences for user %s", userUUID)
	c.JSON(http.StatusCreated, prefs)
}

// UpdateCurrentUserPreferences handles PUT /me/preferences
func (s *Server) UpdateCurrentUserPreferences(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	logger.Info("[PREFERENCES] UpdateCurrentUserPreferences called")

	userUUID := c.GetString("userInternalUUID")
	if userUUID == "" {
		logger.Warn("[PREFERENCES] No user UUID in context")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "user not authenticated",
		})
		return
	}

	// Read request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Error("[PREFERENCES] Failed to read request body: %v", err)
		HandleRequestError(c, InvalidInputError("failed to read request body"))
		return
	}

	// Validate preferences
	if err := validatePreferences(body); err != nil {
		logger.Warn("[PREFERENCES] Validation failed: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Get database from ThreatModelStore
	dbStore, ok := ThreatModelStore.(*GormThreatModelStore)
	if !ok {
		logger.Error("[PREFERENCES] ThreatModelStore is not a database store")
		HandleRequestError(c, ServerError("database not available"))
		return
	}
	db := dbStore.GetDB()

	// Check if preferences exist
	var existing models.UserPreference
	result := db.WithContext(c.Request.Context()).
		Where("user_internal_uuid = ?", userUUID).
		First(&existing)

	now := time.Now()

	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		// Create new preferences
		pref := models.UserPreference{
			ID:               uuid.New().String(),
			UserInternalUUID: userUUID,
			Preferences:      models.JSONRaw(body),
			CreatedAt:        now,
			ModifiedAt:       now,
		}

		if err := db.WithContext(c.Request.Context()).Create(&pref).Error; err != nil {
			logger.Error("[PREFERENCES] Failed to create preferences: %v", err)
			HandleRequestError(c, ServerError("failed to create preferences"))
			return
		}

		logger.Info("[PREFERENCES] Created preferences for user %s via PUT", userUUID)
	} else if result.Error != nil {
		logger.Error("[PREFERENCES] Failed to check existing preferences: %v", result.Error)
		HandleRequestError(c, ServerError("failed to check existing preferences"))
		return
	} else {
		// Update existing preferences
		existing.Preferences = models.JSONRaw(body)
		existing.ModifiedAt = now

		if err := db.WithContext(c.Request.Context()).Save(&existing).Error; err != nil {
			logger.Error("[PREFERENCES] Failed to update preferences: %v", err)
			HandleRequestError(c, ServerError("failed to update preferences"))
			return
		}

		logger.Info("[PREFERENCES] Updated preferences for user %s", userUUID)
	}

	// Parse and return the preferences
	var prefs UserPreferences
	if err := json.Unmarshal(body, &prefs); err != nil {
		logger.Error("[PREFERENCES] Failed to unmarshal preferences for response: %v", err)
		HandleRequestError(c, ServerError("failed to parse preferences"))
		return
	}

	c.JSON(http.StatusOK, prefs)
}

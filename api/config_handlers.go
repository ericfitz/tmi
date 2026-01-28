package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// reservedSettingKeys contains setting key names that are reserved for API endpoints
// or other special purposes. These keys cannot be used for user-defined settings.
//
// "migrate" is reserved because POST /admin/settings/migrate is an API endpoint
// that migrates settings from config file/environment to the database.
var reservedSettingKeys = map[string]string{
	"migrate": "reserved for POST /admin/settings/migrate endpoint",
}

// isReservedSettingKey checks if a setting key is reserved
func isReservedSettingKey(key string) (bool, string) {
	if reason, reserved := reservedSettingKeys[key]; reserved {
		return true, reason
	}
	return false, ""
}

// GetClientConfig returns public configuration for client applications
// This is a public endpoint that does not require authentication.
func (s *Server) GetClientConfig(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	// Build response from server configuration and settings
	clientConfig := s.buildClientConfig(ctx, c)

	// Set cache headers (5 minute cache)
	c.Header("Cache-Control", "public, max-age=300")
	c.Header("Vary", "Accept")

	logger.Debug("Returned client configuration")
	c.JSON(http.StatusOK, clientConfig)
}

// buildClientConfig constructs the ClientConfig response from server config and settings
func (s *Server) buildClientConfig(ctx context.Context, c *gin.Context) ClientConfig {
	logger := slogging.Get()

	// Feature flags - check settings service for dynamic values
	websocketEnabled := true
	samlEnabled := false
	webhooksEnabled := true

	if s.settingsService != nil {
		if val, err := s.settingsService.GetBool(ctx, "features.saml_enabled"); err == nil {
			samlEnabled = val
		}
		if val, err := s.settingsService.GetBool(ctx, "features.webhooks_enabled"); err == nil {
			webhooksEnabled = val
		}
	}

	// Operator info from context (set by middleware)
	var operatorName, operatorContact *string
	if name, exists := c.Get("operatorName"); exists {
		if nameStr, ok := name.(string); ok && nameStr != "" {
			operatorName = &nameStr
		}
	}
	if contact, exists := c.Get("operatorContact"); exists {
		if contactStr, ok := contact.(string); ok && contactStr != "" {
			operatorContact = &contactStr
		}
	}

	// Limits from settings or defaults
	maxFileUpload := 10
	maxParticipants := 10

	if s.settingsService != nil {
		if val, err := s.settingsService.GetInt(ctx, "upload.max_file_size_mb"); err == nil && val > 0 {
			maxFileUpload = val
		}
		if val, err := s.settingsService.GetInt(ctx, "websocket.max_participants"); err == nil && val > 0 {
			maxParticipants = val
		}
	}

	// UI defaults from settings
	defaultTheme := Auto // Auto, Light, Dark are the ClientConfigUiDefaultTheme constants
	if s.settingsService != nil {
		if val, err := s.settingsService.GetString(ctx, "ui.default_theme"); err == nil && val != "" {
			switch val {
			case "light":
				defaultTheme = Light
			case "dark":
				defaultTheme = Dark
			default:
				defaultTheme = Auto
			}
		}
	}

	logger.Debug("Built client config with websocket=%v, saml=%v, webhooks=%v", websocketEnabled, samlEnabled, webhooksEnabled)

	return ClientConfig{
		Features: &struct {
			SamlEnabled      *bool `json:"saml_enabled,omitempty"`
			WebhooksEnabled  *bool `json:"webhooks_enabled,omitempty"`
			WebsocketEnabled *bool `json:"websocket_enabled,omitempty"`
		}{
			WebsocketEnabled: &websocketEnabled,
			SamlEnabled:      &samlEnabled,
			WebhooksEnabled:  &webhooksEnabled,
		},
		Operator: &struct {
			Contact *string `json:"contact,omitempty"`
			Name    *string `json:"name,omitempty"`
		}{
			Name:    operatorName,
			Contact: operatorContact,
		},
		Limits: &struct {
			MaxDiagramParticipants *int `json:"max_diagram_participants,omitempty"`
			MaxFileUploadMb        *int `json:"max_file_upload_mb,omitempty"`
		}{
			MaxFileUploadMb:        &maxFileUpload,
			MaxDiagramParticipants: &maxParticipants,
		},
		Ui: &struct {
			DefaultTheme *ClientConfigUiDefaultTheme `json:"default_theme,omitempty"`
		}{
			DefaultTheme: &defaultTheme,
		},
	}
}

// ListSystemSettings returns all system settings (admin only)
func (s *Server) ListSystemSettings(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	// Check admin permissions
	isAdmin, err := IsUserAdministrator(c)
	if err != nil || !isAdmin {
		logger.Warn("Non-admin user attempted to list system settings")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Administrator access required",
		})
		return
	}

	if s.settingsService == nil {
		logger.Error("Settings service not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "service_unavailable",
			Message: "Settings service unavailable",
		})
		return
	}

	settings, err := s.settingsService.List(ctx)
	if err != nil {
		logger.Error("Failed to list system settings: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "internal_error",
			Message: "Failed to list settings",
		})
		return
	}

	// Convert to API response format
	response := make([]SystemSetting, 0, len(settings))
	for _, setting := range settings {
		response = append(response, modelToAPISystemSetting(setting))
	}

	logger.Info("Listed %d system settings", len(response))
	c.JSON(http.StatusOK, response)
}

// GetSystemSetting returns a specific system setting by key (admin only)
func (s *Server) GetSystemSetting(c *gin.Context, key string) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	// Check for reserved keys (e.g., "migrate" is reserved for the migrate endpoint)
	if reserved, reason := isReservedSettingKey(key); reserved {
		logger.Warn("Attempted to get reserved setting key: %s (%s)", key, reason)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "reserved_key",
			Message: "Setting key '" + key + "' is reserved: " + reason,
		})
		return
	}

	// Check admin permissions
	isAdmin, err := IsUserAdministrator(c)
	if err != nil || !isAdmin {
		logger.Warn("Non-admin user attempted to get system setting: %s", key)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Administrator access required",
		})
		return
	}

	if s.settingsService == nil {
		logger.Error("Settings service not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "service_unavailable",
			Message: "Settings service unavailable",
		})
		return
	}

	setting, err := s.settingsService.Get(ctx, key)
	if err != nil {
		logger.Error("Failed to get system setting %s: %v", key, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "internal_error",
			Message: "Failed to get setting",
		})
		return
	}

	if setting == nil {
		logger.Debug("System setting not found: %s", key)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Setting not found",
		})
		return
	}

	logger.Debug("Retrieved system setting: %s", key)
	c.JSON(http.StatusOK, modelToAPISystemSetting(*setting))
}

// UpdateSystemSetting creates or updates a system setting (admin only)
func (s *Server) UpdateSystemSetting(c *gin.Context, key string) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	// Check for reserved keys (e.g., "migrate" is reserved for the migrate endpoint)
	if reserved, reason := isReservedSettingKey(key); reserved {
		logger.Warn("Attempted to update reserved setting key: %s (%s)", key, reason)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "reserved_key",
			Message: "Setting key '" + key + "' is reserved: " + reason,
		})
		return
	}

	// Check admin permissions
	isAdmin, err := IsUserAdministrator(c)
	if err != nil || !isAdmin {
		logger.Warn("Non-admin user attempted to update system setting: %s", key)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Administrator access required",
		})
		return
	}

	if s.settingsService == nil {
		logger.Error("Settings service not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "service_unavailable",
			Message: "Settings service unavailable",
		})
		return
	}

	// Parse request body
	var req SystemSettingUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid request body for setting update: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Get current user UUID for modified_by
	var modifiedBy *string
	if userUUID, exists := c.Get("userInternalUUID"); exists {
		if uuidStr, ok := userUUID.(string); ok {
			modifiedBy = &uuidStr
		}
	}

	// Convert to model
	setting := models.SystemSetting{
		Key:        key,
		Value:      req.Value,
		Type:       string(req.Type),
		ModifiedAt: time.Now(),
		ModifiedBy: modifiedBy,
	}
	if req.Description != nil {
		setting.Description = req.Description
	}

	// Save the setting
	if err := s.settingsService.Set(ctx, &setting); err != nil {
		logger.Error("Failed to update system setting %s: %v", key, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "validation_error",
			Message: err.Error(),
		})
		return
	}

	logger.Info("Updated system setting: %s", key)
	c.JSON(http.StatusOK, modelToAPISystemSetting(setting))
}

// DeleteSystemSetting deletes a system setting (admin only)
func (s *Server) DeleteSystemSetting(c *gin.Context, key string) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	// Check for reserved keys (e.g., "migrate" is reserved for the migrate endpoint)
	if reserved, reason := isReservedSettingKey(key); reserved {
		logger.Warn("Attempted to delete reserved setting key: %s (%s)", key, reason)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "reserved_key",
			Message: "Setting key '" + key + "' is reserved: " + reason,
		})
		return
	}

	// Check admin permissions
	isAdmin, err := IsUserAdministrator(c)
	if err != nil || !isAdmin {
		logger.Warn("Non-admin user attempted to delete system setting: %s", key)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Administrator access required",
		})
		return
	}

	if s.settingsService == nil {
		logger.Error("Settings service not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "service_unavailable",
			Message: "Settings service unavailable",
		})
		return
	}

	// Check if setting exists
	setting, err := s.settingsService.Get(ctx, key)
	if err != nil {
		logger.Error("Failed to check system setting %s: %v", key, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "internal_error",
			Message: "Failed to check setting",
		})
		return
	}

	if setting == nil {
		logger.Debug("System setting not found for deletion: %s", key)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Setting not found",
		})
		return
	}

	// Delete the setting
	if err := s.settingsService.Delete(ctx, key); err != nil {
		logger.Error("Failed to delete system setting %s: %v", key, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "internal_error",
			Message: "Failed to delete setting",
		})
		return
	}

	logger.Info("Deleted system setting: %s", key)
	c.Status(http.StatusNoContent)
}

// MigrateSystemSettings migrates settings from configuration to database (admin only)
func (s *Server) MigrateSystemSettings(c *gin.Context, params MigrateSystemSettingsParams) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	// Reject unexpected request bodies for defense-in-depth
	// This endpoint uses only query parameters per OpenAPI spec
	if c.Request.ContentLength > 0 {
		logger.Warn("Unexpected request body in settings migration request")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "This endpoint does not accept a request body",
		})
		return
	}

	// Check admin permissions
	isAdmin, err := IsUserAdministrator(c)
	if err != nil || !isAdmin {
		logger.Warn("Non-admin user attempted to migrate system settings")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Administrator access required",
		})
		return
	}

	if s.settingsService == nil {
		logger.Error("Settings service not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "service_unavailable",
			Message: "Settings service unavailable",
		})
		return
	}

	if s.configProvider == nil {
		logger.Error("Config provider not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "service_unavailable",
			Message: "Config provider unavailable",
		})
		return
	}

	// Get overwrite flag (default false)
	overwrite := false
	if params.Overwrite != nil {
		overwrite = *params.Overwrite
	}

	// Get current user UUID for modified_by
	var modifiedBy *string
	if userUUID, exists := c.Get("userInternalUUID"); exists {
		if uuidStr, ok := userUUID.(string); ok {
			modifiedBy = &uuidStr
		}
	}

	// Get migratable settings from config
	migratableSettings := s.configProvider.GetMigratableSettings()

	var migrated []SystemSetting
	var skipped int

	for _, ms := range migratableSettings {
		// Check if setting already exists
		existing, err := s.settingsService.Get(ctx, ms.Key)
		if err != nil {
			logger.Error("Failed to check existing setting %s: %v", ms.Key, err)
			continue
		}

		// Skip if exists and not overwriting
		if existing != nil && !overwrite {
			skipped++
			logger.Debug("Skipping existing setting: %s (overwrite=false)", ms.Key)
			continue
		}

		// Create or update the setting
		description := ms.Description
		setting := models.SystemSetting{
			Key:         ms.Key,
			Value:       ms.Value,
			Type:        ms.Type,
			Description: &description,
			ModifiedAt:  time.Now(),
			ModifiedBy:  modifiedBy,
		}

		if err := s.settingsService.Set(ctx, &setting); err != nil {
			logger.Error("Failed to migrate setting %s: %v", ms.Key, err)
			continue
		}

		migrated = append(migrated, modelToAPISystemSetting(setting))
		logger.Info("Migrated setting: %s = %s", ms.Key, ms.Value)
	}

	logger.Info("Settings migration completed: %d migrated, %d skipped", len(migrated), skipped)

	c.JSON(http.StatusOK, gin.H{
		"migrated": len(migrated),
		"skipped":  skipped,
		"settings": migrated,
	})
}

// modelToAPISystemSetting converts a models.SystemSetting to an API SystemSetting
func modelToAPISystemSetting(m models.SystemSetting) SystemSetting {
	setting := SystemSetting{
		Key:        m.Key,
		Value:      m.Value,
		Type:       SystemSettingType(m.Type),
		ModifiedAt: &m.ModifiedAt,
	}
	if m.Description != nil {
		setting.Description = m.Description
	}
	if m.ModifiedBy != nil {
		// Convert string UUID to openapi_types.UUID
		if parsedUUID, err := uuid.Parse(*m.ModifiedBy); err == nil {
			setting.ModifiedBy = &parsedUUID
		}
	}
	return setting
}

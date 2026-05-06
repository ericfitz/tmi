package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// secretMaskConfigured is the masked value displayed when a secret setting has a value.
const secretMaskConfigured = "<configured>"

// secretMaskNotConfigured is the masked value displayed when a secret setting has no value.
const secretMaskNotConfigured = "<not configured>"

// providerKeyPrefixes are the settings key prefixes for auth providers.
var providerKeyPrefixes = []string{
	"auth.oauth.providers.",
	"auth.saml.providers.",
}

// providerSecretSuffixes are the settings key suffixes for provider secrets.
var providerSecretSuffixes = []string{
	".client_secret",
	".sp_private_key",
	".sp_certificate",
	".idp_metadata_b64xml",
}

// invalidateProviderCacheIfNeeded checks if the key is a provider-related setting
// and invalidates the provider registry cache if so.
func (s *Server) invalidateProviderCacheIfNeeded(key string) {
	if s.providerRegistry == nil {
		return
	}
	for _, prefix := range providerKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			s.providerRegistry.InvalidateCache()
			return
		}
	}
}

// isProviderSecretKey returns true if the key is a provider secret that should be masked.
func isProviderSecretKey(key string) bool {
	isProviderKey := false
	for _, prefix := range providerKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			isProviderKey = true
			break
		}
	}
	if !isProviderKey {
		return false
	}
	for _, suffix := range providerSecretSuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

// extractProviderID extracts the provider ID from a settings key of the form
// "<prefix><id>.<field>", returning the id portion.
func extractProviderID(key, prefix string) string {
	remainder := key[len(prefix):]
	dotIdx := strings.Index(remainder, ".")
	if dotIdx <= 0 {
		return ""
	}
	return remainder[:dotIdx]
}

// validateProviderEnableKey checks if the key is an enable key for a provider
// and validates required fields if the value is "true".
// Returns an error message if validation fails, or empty string if OK.
func (s *Server) validateProviderEnableKey(ctx context.Context, key, value string) string {
	if value != boolTrue {
		return ""
	}

	if strings.HasPrefix(key, "auth.oauth.providers.") && strings.HasSuffix(key, ".enabled") {
		providerID := extractProviderID(key, "auth.oauth.providers.")
		if providerID == "" {
			return ""
		}

		settings, err := s.settingsService.ListByPrefix(ctx, "auth.oauth.providers."+providerID+".")
		if err != nil {
			return "failed to read provider settings"
		}

		providerSettings := make([]auth.ProviderSetting, len(settings))
		for i, setting := range settings {
			providerSettings[i] = auth.ProviderSetting{Key: setting.SettingKey, Value: setting.Value}
		}
		providers := auth.AssembleOAuthProviders(providerSettings)
		p := providers[providerID]

		missing := auth.ValidateOAuthProvider(p)
		if len(missing) > 0 {
			return fmt.Sprintf("Cannot enable OAuth provider %q: missing required fields: %s",
				providerID, strings.Join(missing, ", "))
		}
	}

	if strings.HasPrefix(key, "auth.saml.providers.") && strings.HasSuffix(key, ".enabled") {
		providerID := extractProviderID(key, "auth.saml.providers.")
		if providerID == "" {
			return ""
		}

		settings, err := s.settingsService.ListByPrefix(ctx, "auth.saml.providers."+providerID+".")
		if err != nil {
			return "failed to read provider settings"
		}

		providerSettings := make([]auth.ProviderSetting, len(settings))
		for i, setting := range settings {
			providerSettings[i] = auth.ProviderSetting{Key: setting.SettingKey, Value: setting.Value}
		}
		providers := auth.AssembleSAMLProviders(providerSettings)
		p := providers[providerID]

		missing := auth.ValidateSAMLProvider(p)
		if len(missing) > 0 {
			return fmt.Sprintf("Cannot enable SAML provider %q: missing required fields: %s",
				providerID, strings.Join(missing, ", "))
		}
	}

	return ""
}

// reservedSettingKeys contains setting key names that are reserved for API endpoints
// or other special purposes. These keys cannot be used for user-defined settings.
var reservedSettingKeys = map[string]string{
	"reencrypt": "reserved for POST /admin/settings/reencrypt endpoint",
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

	// Build content providers from registry + delegated overrides
	var contentOAuthCfg *config.ContentOAuthConfig
	if s.contentOAuth != nil {
		contentOAuthCfg = &s.contentOAuth.Cfg
	}
	contentProviders := buildContentProviders(s.contentSourceRegistry, contentOAuthCfg)

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
		ContentProviders: &contentProviders,
	}
}

// configSettingToAPI converts a MigratableSetting to an API SystemSetting with source/read_only.
func configSettingToAPI(cs MigratableSetting) SystemSetting {
	source := SystemSettingSource(cs.Source)
	readOnly := true

	value := cs.Value
	if cs.Secret {
		if value != "" {
			value = secretMaskConfigured
		} else {
			value = secretMaskNotConfigured
		}
	}

	setting := SystemSetting{
		Key:      cs.Key,
		Value:    value,
		Type:     SystemSettingType(cs.Type),
		Source:   &source,
		ReadOnly: &readOnly,
	}
	if cs.Description != "" {
		setting.Description = &cs.Description
	}
	return setting
}

// mergeSettingsWithConfig builds a merged view of database and config settings.
// Config settings take priority over database settings for the same key.
func (s *Server) mergeSettingsWithConfig(dbSettings []models.SystemSetting) []SystemSetting {
	configMap := make(map[string]MigratableSetting)
	if s.configProvider != nil {
		for _, cs := range s.configProvider.GetMigratableSettings() {
			configMap[cs.Key] = cs
		}
	}

	dbMap := make(map[string]models.SystemSetting)
	for _, ds := range dbSettings {
		dbMap[ds.SettingKey] = ds
	}

	allKeys := make(map[string]bool)
	for k := range configMap {
		allKeys[k] = true
	}
	for k := range dbMap {
		allKeys[k] = true
	}

	result := make([]SystemSetting, 0, len(allKeys))
	for key := range allKeys {
		configSetting, inConfig := configMap[key]
		_, inDB := dbMap[key]

		if inConfig {
			result = append(result, configSettingToAPI(configSetting))
		} else if inDB {
			apiSetting := modelToAPISystemSetting(dbMap[key])
			source := SystemSettingSource("database")
			apiSetting.Source = &source
			readOnly := false
			apiSetting.ReadOnly = &readOnly
			if isProviderSecretKey(key) {
				if apiSetting.Value != "" {
					apiSetting.Value = secretMaskConfigured
				} else {
					apiSetting.Value = secretMaskNotConfigured
				}
			}
			result = append(result, apiSetting)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})

	return result
}

// ListSystemSettings returns all system settings (admin only)
func (s *Server) ListSystemSettings(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

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

	// Merge database settings with config settings
	response := s.mergeSettingsWithConfig(settings)

	logger.Info("Listed %d system settings (merged)", len(response))
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

	if s.settingsService == nil {
		logger.Error("Settings service not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "service_unavailable",
			Message: "Settings service unavailable",
		})
		return
	}

	// Check config provider first
	if s.configProvider != nil {
		for _, cs := range s.configProvider.GetMigratableSettings() {
			if cs.Key == key {
				c.JSON(http.StatusOK, configSettingToAPI(cs))
				return
			}
		}
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

	apiSetting := modelToAPISystemSetting(*setting)
	source := SystemSettingSource("database")
	apiSetting.Source = &source
	readOnly := false
	apiSetting.ReadOnly = &readOnly
	if isProviderSecretKey(key) {
		if apiSetting.Value != "" {
			apiSetting.Value = secretMaskConfigured
		} else {
			apiSetting.Value = secretMaskNotConfigured
		}
	}
	logger.Debug("Retrieved system setting: %s", key)
	c.JSON(http.StatusOK, apiSetting)
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

	if s.settingsService == nil {
		logger.Error("Settings service not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "service_unavailable",
			Message: "Settings service unavailable",
		})
		return
	}

	// Check if setting is controlled by config/environment
	if s.configProvider != nil {
		for _, cs := range s.configProvider.GetMigratableSettings() {
			if cs.Key == key {
				logger.Warn("Attempted to update config-controlled setting: %s (source: %s)", key, cs.Source)
				HandleRequestError(c, &RequestError{
					Status:  http.StatusConflict,
					Code:    "conflict",
					Message: "Setting '" + key + "' is controlled by " + cs.Source + " and cannot be modified via the API",
				})
				return
			}
		}
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
		SettingKey:  key,
		Value:       req.Value,
		SettingType: string(req.Type),
		ModifiedAt:  time.Now(),
		ModifiedBy:  modifiedBy,
	}
	if req.Description != nil {
		setting.Description = req.Description
	}

	// Enable-validation gate: validate required fields when enabling a provider
	if validationErr := s.validateProviderEnableKey(ctx, key, setting.Value); validationErr != "" {
		c.JSON(http.StatusConflict, gin.H{"error": validationErr})
		return
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

	s.invalidateProviderCacheIfNeeded(key)

	apiSetting := modelToAPISystemSetting(setting)
	source := SystemSettingSource("database")
	apiSetting.Source = &source
	readOnly := false
	apiSetting.ReadOnly = &readOnly
	logger.Info("Updated system setting: %s", key)
	c.JSON(http.StatusOK, apiSetting)
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
			Message: "Setting not found in database",
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

	s.invalidateProviderCacheIfNeeded(key)

	logger.Info("Deleted system setting: %s", key)
	c.Status(http.StatusNoContent)
}

// ReencryptSystemSettings re-encrypts all system settings with the current encryption key (admin only)
func (s *Server) ReencryptSystemSettings(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	// Reject unexpected request bodies
	if c.Request.ContentLength > 0 {
		logger.Warn("Unexpected request body in settings re-encryption request")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "This endpoint does not accept a request body",
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

	// Get current user UUID for modified_by
	var modifiedBy *string
	if userUUID, exists := c.Get("userInternalUUID"); exists {
		if uuidStr, ok := userUUID.(string); ok {
			modifiedBy = &uuidStr
		}
	}

	reencrypted, settingErrors, err := s.settingsService.ReEncryptAll(ctx, modifiedBy)
	if err != nil {
		// Encryption not enabled returns 409 Conflict
		logger.Warn("Re-encryption failed: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusConflict,
			Code:    "encryption_not_enabled",
			Message: err.Error(),
		})
		return
	}

	logger.Info("Re-encryption completed: %d re-encrypted, %d errors", reencrypted, len(settingErrors))

	c.JSON(http.StatusOK, gin.H{
		"reencrypted": reencrypted,
		"errors":      settingErrors,
		"total":       reencrypted + len(settingErrors),
	})
}

// buildContentProviders constructs the ClientConfig.ContentProviders array
// from the configured ContentSourceRegistry. The kind/default-name/default-icon
// for each source come from the static contentProviderMetaTable; for delegated
// providers, operator-supplied name/icon in cfg.Providers[id] take precedence
// over the defaults.
//
// Returns an empty (non-nil) slice when the registry is nil or empty so the
// JSON response renders a deterministic [] rather than null.
func buildContentProviders(sources *ContentSourceRegistry, cfg *config.ContentOAuthConfig) []ContentProvider {
	out := make([]ContentProvider, 0)
	if sources == nil {
		return out
	}
	for _, id := range sources.Names() {
		meta := lookupContentProviderMeta(id)
		name := meta.DefaultName
		icon := meta.DefaultIcon
		if meta.Kind == "delegated" && cfg != nil {
			if override, ok := cfg.Providers[id]; ok {
				if override.Name != "" {
					name = override.Name
				}
				if override.Icon != "" {
					icon = override.Icon
				}
			}
		}
		out = append(out, ContentProvider{
			Id:   id,
			Name: name,
			Kind: ContentProviderKind(meta.Kind),
			Icon: icon,
		})
	}
	return out
}

// modelToAPISystemSetting converts a models.SystemSetting to an API SystemSetting
func modelToAPISystemSetting(m models.SystemSetting) SystemSetting {
	setting := SystemSetting{
		Key:        m.SettingKey,
		Value:      m.Value,
		Type:       SystemSettingType(m.SettingType),
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

package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
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

// providerKeyPrefixes are the settings key prefixes for auth providers. Used
// for provider-registry cache invalidation; API-response masking uses
// secretMaskKeyPrefixes, a superset.
var providerKeyPrefixes = []string{
	"auth.oauth.providers.",
	"auth.saml.providers.",
}

// secretMaskKeyPrefixes are the dynamic-cardinality provider-family key
// prefixes whose secret-suffixed keys are masked in API responses. A superset
// of providerKeyPrefixes: content_oauth providers store client secrets too,
// but they do not feed the auth provider-registry cache.
var secretMaskKeyPrefixes = []string{
	"auth.oauth.providers.",
	"auth.saml.providers.",
	"content_oauth.providers.",
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
// SEM@8021854ca7c2ed0ff1bf92d0c81d12f62e8ee616: invalidate the auth provider registry cache if the setting key matches a provider prefix (mutates shared state)
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

// isProviderSecretKey returns true if the key is a provider secret that should
// be masked.
//
// Provider subtrees are dynamic-cardinality, so the classification registry
// classifies them by prefix with Secret:false — per-key secrecy lives on the
// migratable setting, not in the registry (see prefixClassifications in
// internal/config/classification_registry.go). This prefix+suffix heuristic
// supplies the per-key judgment the registry cannot express; registry-driven
// masking lives in shouldMaskSettingValue.
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: report whether a settings key is a provider secret that must be masked in API responses (pure)
func isProviderSecretKey(key string) bool {
	isProviderKey := false
	for _, prefix := range secretMaskKeyPrefixes {
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

// shouldMaskSettingValue reports whether a DB-sourced setting's value must be
// masked in API responses. The classification registry's Secret flag is the
// primary source of truth ("The Secret flag drives API-response masking" —
// classification_registry.go); the provider prefix+suffix heuristic is
// unioned in — not used as a mere fallback — because provider subtrees are
// prefix-classified with Secret:false (see isProviderSecretKey), so a
// registry-only check would unmask every auth-provider secret this file
// already masks.
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: report whether a setting's value must be masked in API responses (pure)
func shouldMaskSettingValue(key string) bool {
	return config.ClassificationFor(key).Secret || isProviderSecretKey(key)
}

// extractProviderID extracts the provider ID from a settings key of the form
// "<prefix><id>.<field>", returning the id portion.
// SEM@249b01f4efeacd03d268f0b69702df01a80d6cd1: extract the provider ID segment from a prefixed settings key (pure)
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
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: validate required fields on an OAuth or SAML provider before enabling it (reads DB)
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
			providerSettings[i] = auth.ProviderSetting{Key: string(setting.SettingKey), Value: string(setting.Value)}
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
			providerSettings[i] = auth.ProviderSetting{Key: string(setting.SettingKey), Value: string(setting.Value)}
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
// SEM@f25790d896e8e128807a3c9a0a517fcbe6f710fe: report whether a settings key is reserved for an API endpoint or special purpose (pure)
func isReservedSettingKey(key string) (bool, string) {
	if reason, reserved := reservedSettingKeys[key]; reserved {
		return true, reason
	}
	return false, ""
}

// boolSettingIfPresent resolves a boolean setting and reports whether the key
// was actually present. It distinguishes a missing key from a key set to
// "false": GetBool returns (false, nil) for both, which would silently flip a
// default-true flag off when the key is simply unset. By reading the raw string
// value first (which is "" only when the key resolves to nothing through the
// config-provider-then-database cascade) we keep the caller's default intact
// for absent keys.
// SEM@8f7b5125fd7a1b5bb10210ba480278708de918b0: fetch a boolean setting only when the key is explicitly configured, preserving caller defaults (reads DB)
func (s *Server) boolSettingIfPresent(ctx context.Context, key string) (value bool, present bool) {
	if s.settingsService == nil {
		return false, false
	}
	raw, err := s.settingsService.GetString(ctx, key)
	if err != nil || raw == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return parsed, true
}

// GetClientConfig returns public configuration for client applications
// This is a public endpoint that does not require authentication.
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: fetch public client configuration including feature flags, limits, and content providers
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
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: construct the ClientConfig response from server config and settings service (reads DB)
func (s *Server) buildClientConfig(ctx context.Context, c *gin.Context) ClientConfig {
	logger := slogging.Get()

	// Feature flags - check settings service for dynamic values.
	//
	// Read each flag only when its key actually resolves, so a missing key
	// keeps the code default rather than collapsing to GetBool's (false, nil)
	// for "not found" — which would silently flip a default-true flag off.
	websocketEnabled := true
	samlEnabled := false
	webhooksEnabled := true

	if s.settingsService != nil {
		if val, ok := s.boolSettingIfPresent(ctx, "features.saml_enabled"); ok {
			samlEnabled = val
		}
		if val, ok := s.boolSettingIfPresent(ctx, "features.webhooks_enabled"); ok {
			webhooksEnabled = val
		}
		if val, ok := s.boolSettingIfPresent(ctx, "features.websocket_enabled"); ok {
			websocketEnabled = val
		}
	}

	// Operator info from settings service (config file/env > database).
	var operatorName, operatorContact, operatorJurisdiction *string
	if s.settingsService != nil {
		if val, err := s.settingsService.GetString(ctx, "operator.name"); err == nil && val != "" {
			v := val
			operatorName = &v
		}
		if val, err := s.settingsService.GetString(ctx, "operator.contact"); err == nil && val != "" {
			v := val
			operatorContact = &v
		}
		if val, err := s.settingsService.GetString(ctx, "operator.jurisdiction"); err == nil && val != "" {
			v := val
			operatorJurisdiction = &v
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

	// Build content providers from registry + delegated overrides.
	// Resolve the live registry through the holder (runtime-toggleable) when
	// available; fall back to the startup-wired field otherwise.
	var liveRegistry *ContentSourceRegistry
	if b := s.getContentSourceBundle(ctx); b != nil {
		liveRegistry = b.Sources
	}
	var contentOAuthCfg *config.ContentOAuthConfig
	if s.contentOAuth != nil {
		contentOAuthCfg = &s.contentOAuth.Cfg
	}
	contentProviders := buildContentProviders(liveRegistry, contentOAuthCfg, s.contentPickerConfigs)

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
			Contact      *string `json:"contact,omitempty"`
			Jurisdiction *string `json:"jurisdiction,omitempty"`
			Name         *string `json:"name,omitempty"`
		}{
			Name:         operatorName,
			Contact:      operatorContact,
			Jurisdiction: operatorJurisdiction,
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

// filterByVisibility returns the subset of settings whose classification meets
// the requested visibility level.
//
// For VisibilityPublic: only settings classified VisibilityPublic AND not
// Secret are returned — secrets are never exposed on the public endpoint.
//
// For VisibilityAdminOnly: settings classified VisibilityAdminOnly OR
// VisibilityPublic are returned (the admin view is a superset of public).
// Secret settings pass through here because the admin endpoint still shows
// them — masked as "<configured>" — so the admin can see a secret field
// exists. VisibilityInternal settings (bootstrap keys like server.port,
// database.url, auth.jwt.secret) are excluded from all API responses.
//
// Settings whose key is unclassified (zero ConfigClass, CategoryUnclassified)
// are treated as VisibilityInternal and filtered out.
// SEM@dcb09b6afcb6a3a78ce7ba3c345e459ba9cf55a2: filter settings to those whose visibility classification matches the requested level (pure)
func filterByVisibility(settings []MigratableSetting, level config.Visibility) []MigratableSetting {
	out := make([]MigratableSetting, 0, len(settings))
	for _, s := range settings {
		cls := config.ClassificationFor(s.Key)
		switch level {
		case config.VisibilityPublic:
			if cls.Visibility == config.VisibilityPublic && !cls.Secret {
				out = append(out, s)
			}
		case config.VisibilityAdminOnly:
			if cls.Visibility == config.VisibilityAdminOnly || cls.Visibility == config.VisibilityPublic {
				out = append(out, s)
			}
		}
	}
	return out
}

// configSettingToAPI converts a MigratableSetting to an API SystemSetting with source/read_only.
// SEM@249b01f4efeacd03d268f0b69702df01a80d6cd1: convert a migratable config setting to an API SystemSetting, masking secrets (pure)
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
// Settings classified VisibilityInternal are excluded: they are bootstrap or
// server-internal settings that must never appear in any API response.
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: build a merged admin-visible settings list with config-file values overriding database values (reads DB)
func (s *Server) mergeSettingsWithConfig(dbSettings []models.SystemSetting) []SystemSetting {
	configMap := make(map[string]MigratableSetting)
	if s.configProvider != nil {
		// Filter out VisibilityInternal settings before building the config map so
		// that bootstrap keys (server.port, database.url, auth.jwt.secret, etc.)
		// are never returned by the admin /admin/settings endpoint.
		allConfigSettings := s.configProvider.GetMigratableSettings()
		visible := filterByVisibility(allConfigSettings, config.VisibilityAdminOnly)
		for _, cs := range visible {
			configMap[cs.Key] = cs
		}
	}

	dbMap := make(map[string]models.SystemSetting)
	for _, ds := range dbSettings {
		dbMap[string(ds.SettingKey)] = ds
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
			if shouldMaskSettingValue(key) {
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
// SEM@91dca85b52bdc03010be6f156c266607fa22df98: list all admin-visible system settings merged from config and database (reads DB)
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
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: fetch a single system setting by key, masking secrets and hiding internal keys (reads DB)
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

	// VisibilityInternal settings are never surfaced via the API, regardless of
	// category. This covers bootstrap keys (file/env-only, never DB-stored) and
	// any internal-operational keys, as well as unclassified keys (zero
	// ConfigClass = VisibilityInternal). Treat them as not found.
	if config.ClassificationFor(key).Visibility == config.VisibilityInternal {
		logger.Debug("System setting not found (internal-visibility key, not API-visible): %s", key)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Setting not found",
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
	if shouldMaskSettingValue(key) {
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
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: create or update a database system setting, validating provider enables and invalidating cache (reads DB)
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
		SettingKey:  models.DBVarchar(key),
		Value:       models.DBText(req.Value),
		SettingType: models.DBVarchar(string(req.Type)),
		ModifiedAt:  time.Now(),
		ModifiedBy:  models.NewNullableDBVarchar(modifiedBy),
		Description: models.NewNullableDBText(req.Description),
	}

	// Enable-validation gate: validate required fields when enabling a provider
	if validationErr := s.validateProviderEnableKey(ctx, key, string(setting.Value)); validationErr != "" {
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
// SEM@dcb09b6afcb6a3a78ce7ba3c345e459ba9cf55a2: delete a system setting by key, rejecting reserved or internal-visibility keys (reads DB)
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

	// VisibilityInternal settings are never surfaced or mutated via the API,
	// regardless of category. This covers bootstrap keys (file/env-only, never
	// DB-stored, nothing to delete) and any internal-operational keys, as well
	// as unclassified keys (zero ConfigClass = VisibilityInternal). Treat them
	// as not found.
	if config.ClassificationFor(key).Visibility == config.VisibilityInternal {
		logger.Debug("System setting not found for deletion (internal-visibility key, not API-visible): %s", key)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Setting not found in database",
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
// SEM@91dca85b52bdc03010be6f156c266607fa22df98: re-encrypt all stored system settings with the current encryption key (reads DB)
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
// pickerConfigs supplies browser-safe picker bootstrap values keyed by source
// id. When a non-empty map is present for an id, it is attached as
// ContentProvider.picker_config; otherwise the field is omitted from the
// response.
//
// Returns an empty (non-nil) slice when the registry is nil or empty so the
// JSON response renders a deterministic [] rather than null.
// SEM@f2e01937e40c91e87ac47a34d11870fde716d093: build the content-provider list from the source registry, merging operator name/icon overrides (pure)
func buildContentProviders(sources *ContentSourceRegistry, cfg *config.ContentOAuthConfig, pickerConfigs map[string]map[string]string) []ContentProvider {
	out := make([]ContentProvider, 0)
	if sources == nil {
		return out
	}
	for _, id := range sources.Names() {
		meta := lookupContentProviderMeta(id)
		name := meta.DefaultName
		icon := meta.DefaultIcon
		if meta.Kind == ContentProviderKindDelegated && cfg != nil {
			if override, ok := cfg.Providers[id]; ok {
				if override.Name != "" {
					name = override.Name
				}
				if override.Icon != "" {
					icon = override.Icon
				}
			}
		}
		provider := ContentProvider{
			Id:   id,
			Name: name,
			Kind: meta.Kind,
			Icon: icon,
		}
		if pc, ok := pickerConfigs[id]; ok && len(pc) > 0 {
			pcCopy := make(map[string]string, len(pc))
			for k, v := range pc {
				pcCopy[k] = v
			}
			provider.PickerConfig = &pcCopy
		}
		out = append(out, provider)
	}
	return out
}

// modelToAPISystemSetting converts a models.SystemSetting to an API SystemSetting
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: convert a DB system-setting model to its API DTO (pure)
func modelToAPISystemSetting(m models.SystemSetting) SystemSetting {
	setting := SystemSetting{
		Key:         string(m.SettingKey),
		Value:       string(m.Value),
		Type:        SystemSettingType(m.SettingType),
		ModifiedAt:  &m.ModifiedAt,
		Description: m.Description.Ptr(),
	}
	if m.ModifiedBy.Valid {
		// Convert string UUID to openapi_types.UUID
		if parsedUUID, err := uuid.Parse(m.ModifiedBy.String); err == nil {
			setting.ModifiedBy = &parsedUUID
		}
	}
	return setting
}

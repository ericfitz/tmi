package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// SettingsService provides access to system settings with caching.
//
// Configuration Priority:
// When a ConfigProvider is set, the service implements a three-tier configuration
// priority system: environment variables > config file > database.
//
// The ConfigProvider (via GetMigratableSettings) already merges environment variables
// with config file values (environment takes precedence). When retrieving a setting,
// if a value exists in the ConfigProvider, it is returned. Otherwise, the database
// value is used. This allows operators to:
//   - Override any setting via environment variables (highest priority)
//   - Set defaults in config files (medium priority)
//   - Store runtime-configurable values in the database (lowest priority)
//
// The database serves as the persistent store for settings that can be modified
// at runtime via the admin API, while environment/config values always win.
type SettingsService struct {
	gormDB  *gorm.DB
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder

	// In-memory cache for when Redis is unavailable
	memCache    map[string]settingsCacheEntry
	memCacheMu  sync.RWMutex
	memCacheTTL time.Duration
	useMemCache bool

	// Config provider for environment/config file values (takes priority over database)
	configProvider ConfigProvider
	// Cached config settings map for fast lookups
	configSettingsCache    map[string]MigratableSetting
	configSettingsCacheMu  sync.RWMutex
	configSettingsCacheSet bool

	// Encryptor for at-rest encryption of setting values
	encryptor *crypto.SettingsEncryptor
}

// settingsCacheEntry represents a cached setting value
type settingsCacheEntry struct {
	setting   models.SystemSetting
	expiresAt time.Time
}

// Cache TTL for system settings
const (
	SettingsCacheTTL = 60 * time.Second // Short TTL for settings
	SettingsCacheKey = "tmi:settings:"
)

// NewSettingsService creates a new settings service
func NewSettingsService(gormDB *gorm.DB, redisDB *db.RedisDB) *SettingsService {
	useMemCache := redisDB == nil
	return &SettingsService{
		gormDB:              gormDB,
		redis:               redisDB,
		builder:             db.NewRedisKeyBuilder(),
		memCache:            make(map[string]settingsCacheEntry),
		memCacheTTL:         SettingsCacheTTL,
		useMemCache:         useMemCache,
		configSettingsCache: make(map[string]MigratableSetting),
	}
}

// SetConfigProvider sets the config provider for environment/config file priority lookups.
// When set, GetWithPriority will check config values before database values.
func (s *SettingsService) SetConfigProvider(provider ConfigProvider) {
	s.configSettingsCacheMu.Lock()
	defer s.configSettingsCacheMu.Unlock()

	s.configProvider = provider
	s.configSettingsCacheSet = false // Force cache rebuild on next access
}

// SetEncryptor sets the encryptor for at-rest encryption of setting values.
// When set, values are encrypted before writing to the database and decrypted after reading.
func (s *SettingsService) SetEncryptor(enc *crypto.SettingsEncryptor) {
	s.encryptor = enc
}

// getConfigSetting retrieves a setting from the config provider if available.
// Returns the setting value and true if found, empty string and false otherwise.
func (s *SettingsService) getConfigSetting(key string) (MigratableSetting, bool) {
	if s.configProvider == nil {
		return MigratableSetting{}, false
	}

	// Build cache if not set
	s.configSettingsCacheMu.RLock()
	cacheSet := s.configSettingsCacheSet
	s.configSettingsCacheMu.RUnlock()

	if !cacheSet {
		s.configSettingsCacheMu.Lock()
		// Double-check after acquiring write lock
		if !s.configSettingsCacheSet {
			s.configSettingsCache = make(map[string]MigratableSetting)
			for _, setting := range s.configProvider.GetMigratableSettings() {
				s.configSettingsCache[setting.Key] = setting
			}
			s.configSettingsCacheSet = true
		}
		s.configSettingsCacheMu.Unlock()
	}

	s.configSettingsCacheMu.RLock()
	defer s.configSettingsCacheMu.RUnlock()

	setting, found := s.configSettingsCache[key]
	return setting, found
}

// Get retrieves a setting by key, checking cache first
func (s *SettingsService) Get(ctx context.Context, key string) (*models.SystemSetting, error) {
	logger := slogging.Get()

	// Try cache first
	setting, found := s.getFromCache(ctx, key)
	if found {
		logger.Debug("Settings cache hit for key: %s", key)
		return setting, nil
	}
	logger.Debug("Settings cache miss for key: %s", key)

	// Load from database
	var dbSetting models.SystemSetting
	if err := s.gormDB.WithContext(ctx).Where("key = ?", key).First(&dbSetting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("failed to get setting %s: %w", key, err)
	}

	// Decrypt if encryptor is configured
	if s.encryptor != nil {
		decrypted, err := s.encryptor.Decrypt(dbSetting.Value)
		if err != nil {
			logger.Error("Failed to decrypt setting %s: %v", key, err)
			return nil, fmt.Errorf("failed to decrypt setting %s: %w", key, err)
		}
		dbSetting.Value = decrypted
	}

	// Cache the decrypted result (cache stores plaintext)
	s.setInCache(ctx, &dbSetting)

	return &dbSetting, nil
}

// GetString retrieves a string setting value.
// Priority: environment/config file > database (see SettingsService documentation).
func (s *SettingsService) GetString(ctx context.Context, key string) (string, error) {
	// Check config provider first (environment > config file)
	if configSetting, found := s.getConfigSetting(key); found {
		return configSetting.Value, nil
	}

	// Fall back to database
	setting, err := s.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if setting == nil {
		return "", nil
	}
	return setting.Value, nil
}

// GetInt retrieves an integer setting value.
// Priority: environment/config file > database (see SettingsService documentation).
func (s *SettingsService) GetInt(ctx context.Context, key string) (int, error) {
	// Check config provider first (environment > config file)
	if configSetting, found := s.getConfigSetting(key); found {
		val, err := strconv.Atoi(configSetting.Value)
		if err != nil {
			return 0, fmt.Errorf("config setting %s is not a valid integer: %w", key, err)
		}
		return val, nil
	}

	// Fall back to database
	setting, err := s.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	if setting == nil {
		return 0, nil
	}
	val, err := strconv.Atoi(setting.Value)
	if err != nil {
		return 0, fmt.Errorf("setting %s is not a valid integer: %w", key, err)
	}
	return val, nil
}

// GetBool retrieves a boolean setting value.
// Priority: environment/config file > database (see SettingsService documentation).
func (s *SettingsService) GetBool(ctx context.Context, key string) (bool, error) {
	// Check config provider first (environment > config file)
	if configSetting, found := s.getConfigSetting(key); found {
		val, err := strconv.ParseBool(configSetting.Value)
		if err != nil {
			return false, fmt.Errorf("config setting %s is not a valid boolean: %w", key, err)
		}
		return val, nil
	}

	// Fall back to database
	setting, err := s.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if setting == nil {
		return false, nil
	}
	val, err := strconv.ParseBool(setting.Value)
	if err != nil {
		return false, fmt.Errorf("setting %s is not a valid boolean: %w", key, err)
	}
	return val, nil
}

// GetJSON retrieves a JSON setting value and unmarshals it into the target.
// Priority: environment/config file > database (see SettingsService documentation).
func (s *SettingsService) GetJSON(ctx context.Context, key string, target interface{}) error {
	// Check config provider first (environment > config file)
	if configSetting, found := s.getConfigSetting(key); found {
		if err := json.Unmarshal([]byte(configSetting.Value), target); err != nil {
			return fmt.Errorf("config setting %s is not valid JSON: %w", key, err)
		}
		return nil
	}

	// Fall back to database
	setting, err := s.Get(ctx, key)
	if err != nil {
		return err
	}
	if setting == nil {
		return nil
	}
	if err := json.Unmarshal([]byte(setting.Value), target); err != nil {
		return fmt.Errorf("setting %s is not valid JSON: %w", key, err)
	}
	return nil
}

// List retrieves all settings
func (s *SettingsService) List(ctx context.Context) ([]models.SystemSetting, error) {
	logger := slogging.Get()

	var settings []models.SystemSetting
	if err := s.gormDB.WithContext(ctx).Order("setting_key").Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}

	// Decrypt all values
	if s.encryptor != nil {
		for i := range settings {
			decrypted, err := s.encryptor.Decrypt(settings[i].Value)
			if err != nil {
				logger.Error("Failed to decrypt setting %s: %v", settings[i].SettingKey, err)
				return nil, fmt.Errorf("failed to decrypt setting %s: %w", settings[i].SettingKey, err)
			}
			settings[i].Value = decrypted
		}
	}

	return settings, nil
}

// Set creates or updates a setting
func (s *SettingsService) Set(ctx context.Context, setting *models.SystemSetting) error {
	logger := slogging.Get()

	// Validate setting type
	switch setting.SettingType {
	case models.SystemSettingTypeString, models.SystemSettingTypeInt,
		models.SystemSettingTypeBool, models.SystemSettingTypeJSON:
		// Valid
	default:
		return fmt.Errorf("invalid setting type: %s", setting.SettingType)
	}

	// Validate value matches type (validate plaintext before encryption)
	if err := s.validateValue(setting); err != nil {
		return err
	}

	// Encrypt value before saving to database
	dbSetting := *setting
	if s.encryptor != nil && s.encryptor.IsEnabled() {
		encrypted, err := s.encryptor.Encrypt(setting.Value)
		if err != nil {
			return fmt.Errorf("failed to encrypt setting %s: %w", setting.SettingKey, err)
		}
		dbSetting.Value = encrypted
	}

	// Upsert the setting
	dbSetting.ModifiedAt = time.Now()
	result := s.gormDB.WithContext(ctx).Save(&dbSetting)
	if result.Error != nil {
		return fmt.Errorf("failed to save setting %s: %w", setting.SettingKey, result.Error)
	}

	// Update the caller's ModifiedAt to match what was saved
	setting.ModifiedAt = dbSetting.ModifiedAt

	// Invalidate cache
	s.invalidateCache(ctx, setting.SettingKey)
	logger.Info("Updated system setting: %s", setting.SettingKey)

	return nil
}

// Delete removes a setting
func (s *SettingsService) Delete(ctx context.Context, key string) error {
	logger := slogging.Get()

	result := s.gormDB.WithContext(ctx).Delete(&models.SystemSetting{}, "setting_key = ?", key)
	if result.Error != nil {
		return fmt.Errorf("failed to delete setting %s: %w", key, result.Error)
	}

	// Invalidate cache
	s.invalidateCache(ctx, key)
	logger.Info("Deleted system setting: %s", key)

	return nil
}

// SeedDefaults seeds the default settings if they don't exist
func (s *SettingsService) SeedDefaults(ctx context.Context) error {
	logger := slogging.Get()
	defaults := models.DefaultSystemSettings()

	for _, setting := range defaults {
		// Only insert if not exists (don't overwrite)
		var existing models.SystemSetting
		err := s.gormDB.WithContext(ctx).Where("setting_key = ?", setting.SettingKey).First(&existing).Error
		if err == nil {
			// Already exists, skip
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to check existing setting %s: %w", setting.SettingKey, err)
		}

		// Encrypt value before seeding
		dbSetting := setting
		if s.encryptor != nil && s.encryptor.IsEnabled() {
			encrypted, err := s.encryptor.Encrypt(setting.Value)
			if err != nil {
				return fmt.Errorf("failed to encrypt default setting %s: %w", setting.SettingKey, err)
			}
			dbSetting.Value = encrypted
		}

		// Create the setting
		dbSetting.ModifiedAt = time.Now()
		if err := s.gormDB.WithContext(ctx).Create(&dbSetting).Error; err != nil {
			return fmt.Errorf("failed to seed setting %s: %w", setting.SettingKey, err)
		}
		logger.Debug("Seeded default setting: %s", setting.SettingKey)
	}

	logger.Info("Seeded %d default system settings", len(defaults))
	return nil
}

// SettingError represents an error for a specific setting during re-encryption.
type SettingError struct {
	Key   string `json:"key"`
	Error string `json:"error"`
}

// ReEncryptAll re-encrypts all settings with the current encryption key.
// Returns the count of settings re-encrypted, any per-setting errors, and a fatal error if applicable.
func (s *SettingsService) ReEncryptAll(ctx context.Context, modifiedBy *string) (int, []SettingError, error) {
	logger := slogging.Get()

	if s.encryptor == nil || !s.encryptor.IsEnabled() {
		return 0, nil, fmt.Errorf("encryption is not enabled")
	}

	// Load all settings directly from database (may be encrypted with old key or plaintext)
	var settings []models.SystemSetting
	if err := s.gormDB.WithContext(ctx).Find(&settings).Error; err != nil {
		return 0, nil, fmt.Errorf("failed to list settings for re-encryption: %w", err)
	}

	var reencrypted int
	var settingErrors []SettingError

	for _, setting := range settings {
		// Decrypt (handles both plaintext and encrypted values, tries current then previous key)
		plaintext, err := s.encryptor.Decrypt(setting.Value)
		if err != nil {
			logger.Error("Failed to decrypt setting %s during re-encryption: %v", setting.SettingKey, err)
			settingErrors = append(settingErrors, SettingError{Key: setting.SettingKey, Error: err.Error()})
			continue
		}

		// Re-encrypt with current key
		encrypted, err := s.encryptor.Encrypt(plaintext)
		if err != nil {
			logger.Error("Failed to re-encrypt setting %s: %v", setting.SettingKey, err)
			settingErrors = append(settingErrors, SettingError{Key: setting.SettingKey, Error: err.Error()})
			continue
		}

		// Update in database
		setting.Value = encrypted
		setting.ModifiedAt = time.Now()
		setting.ModifiedBy = modifiedBy
		if err := s.gormDB.WithContext(ctx).Save(&setting).Error; err != nil {
			logger.Error("Failed to save re-encrypted setting %s: %v", setting.SettingKey, err)
			settingErrors = append(settingErrors, SettingError{Key: setting.SettingKey, Error: err.Error()})
			continue
		}

		reencrypted++
	}

	// Invalidate all caches
	s.InvalidateAll(ctx)

	logger.Info("Re-encryption completed: %d re-encrypted, %d errors", reencrypted, len(settingErrors))
	return reencrypted, settingErrors, nil
}

// validateValue validates that the value matches the declared type
func (s *SettingsService) validateValue(setting *models.SystemSetting) error {
	switch setting.SettingType {
	case models.SystemSettingTypeInt:
		if _, err := strconv.Atoi(setting.Value); err != nil {
			return fmt.Errorf("value '%s' is not a valid integer", setting.Value)
		}
	case models.SystemSettingTypeBool:
		if _, err := strconv.ParseBool(setting.Value); err != nil {
			return fmt.Errorf("value '%s' is not a valid boolean", setting.Value)
		}
	case models.SystemSettingTypeJSON:
		var js json.RawMessage
		if err := json.Unmarshal([]byte(setting.Value), &js); err != nil {
			return fmt.Errorf("value '%s' is not valid JSON", setting.Value)
		}
	case models.SystemSettingTypeString:
		// Any string is valid
	}
	return nil
}

// getFromCache retrieves a setting from cache
func (s *SettingsService) getFromCache(ctx context.Context, key string) (*models.SystemSetting, bool) {
	if s.useMemCache {
		return s.getFromMemCache(key)
	}
	return s.getFromRedisCache(ctx, key)
}

// setInCache stores a setting in cache
func (s *SettingsService) setInCache(ctx context.Context, setting *models.SystemSetting) {
	if s.useMemCache {
		s.setInMemCache(setting)
	} else {
		s.setInRedisCache(ctx, setting)
	}
}

// invalidateCache removes a setting from cache
func (s *SettingsService) invalidateCache(ctx context.Context, key string) {
	if s.useMemCache {
		s.invalidateMemCache(key)
	} else {
		s.invalidateRedisCache(ctx, key)
	}
}

// Memory cache methods

func (s *SettingsService) getFromMemCache(key string) (*models.SystemSetting, bool) {
	s.memCacheMu.RLock()
	defer s.memCacheMu.RUnlock()

	entry, ok := s.memCache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return &entry.setting, true
}

func (s *SettingsService) setInMemCache(setting *models.SystemSetting) {
	s.memCacheMu.Lock()
	defer s.memCacheMu.Unlock()

	s.memCache[setting.SettingKey] = settingsCacheEntry{
		setting:   *setting,
		expiresAt: time.Now().Add(s.memCacheTTL),
	}
}

func (s *SettingsService) invalidateMemCache(key string) {
	s.memCacheMu.Lock()
	defer s.memCacheMu.Unlock()

	delete(s.memCache, key)
}

// Redis cache methods

func (s *SettingsService) getFromRedisCache(ctx context.Context, key string) (*models.SystemSetting, bool) {
	logger := slogging.Get()
	cacheKey := SettingsCacheKey + key

	data, err := s.redis.Get(ctx, cacheKey)
	if err != nil {
		if err == redis.Nil {
			return nil, false
		}
		logger.Error("Failed to get setting from Redis cache: %v", err)
		return nil, false
	}

	var setting models.SystemSetting
	if err := json.Unmarshal([]byte(data), &setting); err != nil {
		logger.Error("Failed to unmarshal cached setting: %v", err)
		return nil, false
	}

	return &setting, true
}

func (s *SettingsService) setInRedisCache(ctx context.Context, setting *models.SystemSetting) {
	logger := slogging.Get()
	cacheKey := SettingsCacheKey + setting.SettingKey

	data, err := json.Marshal(setting)
	if err != nil {
		logger.Error("Failed to marshal setting for cache: %v", err)
		return
	}

	if err := s.redis.Set(ctx, cacheKey, data, SettingsCacheTTL); err != nil {
		logger.Error("Failed to cache setting in Redis: %v", err)
	}
}

func (s *SettingsService) invalidateRedisCache(ctx context.Context, key string) {
	logger := slogging.Get()
	cacheKey := SettingsCacheKey + key

	if err := s.redis.Del(ctx, cacheKey); err != nil {
		logger.Error("Failed to invalidate setting cache: %v", err)
	}
}

// InvalidateAll clears all settings from cache
func (s *SettingsService) InvalidateAll(ctx context.Context) {
	if s.useMemCache {
		s.memCacheMu.Lock()
		s.memCache = make(map[string]settingsCacheEntry)
		s.memCacheMu.Unlock()
	}
	// For Redis, we would need to use SCAN to find all keys with the prefix
	// For now, settings will expire naturally with TTL
}

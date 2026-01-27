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
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// SettingsService provides access to system settings with caching
type SettingsService struct {
	gormDB  *gorm.DB
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder

	// In-memory cache for when Redis is unavailable
	memCache    map[string]settingsCacheEntry
	memCacheMu  sync.RWMutex
	memCacheTTL time.Duration
	useMemCache bool
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
		gormDB:      gormDB,
		redis:       redisDB,
		builder:     db.NewRedisKeyBuilder(),
		memCache:    make(map[string]settingsCacheEntry),
		memCacheTTL: SettingsCacheTTL,
		useMemCache: useMemCache,
	}
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

	// Cache the result
	s.setInCache(ctx, &dbSetting)

	return &dbSetting, nil
}

// GetString retrieves a string setting value
func (s *SettingsService) GetString(ctx context.Context, key string) (string, error) {
	setting, err := s.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if setting == nil {
		return "", nil
	}
	return setting.Value, nil
}

// GetInt retrieves an integer setting value
func (s *SettingsService) GetInt(ctx context.Context, key string) (int, error) {
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

// GetBool retrieves a boolean setting value
func (s *SettingsService) GetBool(ctx context.Context, key string) (bool, error) {
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

// GetJSON retrieves a JSON setting value and unmarshals it into the target
func (s *SettingsService) GetJSON(ctx context.Context, key string, target interface{}) error {
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
	var settings []models.SystemSetting
	if err := s.gormDB.WithContext(ctx).Order("key").Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}
	return settings, nil
}

// Set creates or updates a setting
func (s *SettingsService) Set(ctx context.Context, setting *models.SystemSetting) error {
	logger := slogging.Get()

	// Validate setting type
	switch setting.Type {
	case models.SystemSettingTypeString, models.SystemSettingTypeInt,
		models.SystemSettingTypeBool, models.SystemSettingTypeJSON:
		// Valid
	default:
		return fmt.Errorf("invalid setting type: %s", setting.Type)
	}

	// Validate value matches type
	if err := s.validateValue(setting); err != nil {
		return err
	}

	// Upsert the setting
	setting.ModifiedAt = time.Now()
	result := s.gormDB.WithContext(ctx).Save(setting)
	if result.Error != nil {
		return fmt.Errorf("failed to save setting %s: %w", setting.Key, result.Error)
	}

	// Invalidate cache
	s.invalidateCache(ctx, setting.Key)
	logger.Info("Updated system setting: %s", setting.Key)

	return nil
}

// Delete removes a setting
func (s *SettingsService) Delete(ctx context.Context, key string) error {
	logger := slogging.Get()

	result := s.gormDB.WithContext(ctx).Delete(&models.SystemSetting{}, "key = ?", key)
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
		err := s.gormDB.WithContext(ctx).Where("key = ?", setting.Key).First(&existing).Error
		if err == nil {
			// Already exists, skip
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to check existing setting %s: %w", setting.Key, err)
		}

		// Create the setting
		setting.ModifiedAt = time.Now()
		if err := s.gormDB.WithContext(ctx).Create(&setting).Error; err != nil {
			return fmt.Errorf("failed to seed setting %s: %w", setting.Key, err)
		}
		logger.Debug("Seeded default setting: %s = %s", setting.Key, setting.Value)
	}

	logger.Info("Seeded %d default system settings", len(defaults))
	return nil
}

// validateValue validates that the value matches the declared type
func (s *SettingsService) validateValue(setting *models.SystemSetting) error {
	switch setting.Type {
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

	s.memCache[setting.Key] = settingsCacheEntry{
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
	cacheKey := SettingsCacheKey + setting.Key

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

package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsService_MemCache(t *testing.T) {
	// Create a service with only memory cache (no Redis, no GORM)
	service := &SettingsService{
		memCache:    make(map[string]settingsCacheEntry),
		memCacheTTL: 60 * time.Second,
		useMemCache: true,
	}

	t.Run("setInMemCache and getFromMemCache", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.key",
			Value:       "test-value",
			SettingType: models.SystemSettingTypeString,
			ModifiedAt:  time.Now(),
		}

		// Set in cache
		service.setInMemCache(setting)

		// Get from cache
		cached, found := service.getFromMemCache("test.key")
		assert.True(t, found)
		require.NotNil(t, cached)
		assert.Equal(t, "test.key", cached.SettingKey)
		assert.Equal(t, "test-value", cached.Value)
	})

	t.Run("getFromMemCache returns false for missing key", func(t *testing.T) {
		cached, found := service.getFromMemCache("nonexistent.key")
		assert.False(t, found)
		assert.Nil(t, cached)
	})

	t.Run("invalidateMemCache removes entry", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "to.invalidate",
			Value:       "value",
			SettingType: models.SystemSettingTypeString,
			ModifiedAt:  time.Now(),
		}

		// Set and verify
		service.setInMemCache(setting)
		_, found := service.getFromMemCache("to.invalidate")
		assert.True(t, found)

		// Invalidate and verify
		service.invalidateMemCache("to.invalidate")
		_, found = service.getFromMemCache("to.invalidate")
		assert.False(t, found)
	})

	t.Run("expired entries are not returned", func(t *testing.T) {
		// Create service with very short TTL
		shortTTLService := &SettingsService{
			memCache:    make(map[string]settingsCacheEntry),
			memCacheTTL: 1 * time.Millisecond,
			useMemCache: true,
		}

		setting := &models.SystemSetting{
			SettingKey:  "expiring.key",
			Value:       "value",
			SettingType: models.SystemSettingTypeString,
			ModifiedAt:  time.Now(),
		}

		shortTTLService.setInMemCache(setting)

		// Wait for expiration
		time.Sleep(5 * time.Millisecond)

		// Should not find expired entry
		_, found := shortTTLService.getFromMemCache("expiring.key")
		assert.False(t, found)
	})
}

func TestSettingsService_ValidateValue(t *testing.T) {
	service := &SettingsService{}

	t.Run("valid int value", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.int",
			Value:       "100",
			SettingType: models.SystemSettingTypeInt,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})

	t.Run("invalid int value", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.int",
			Value:       "not-an-int",
			SettingType: models.SystemSettingTypeInt,
		}
		err := service.validateValue(setting)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid integer")
	})

	t.Run("valid bool value - true", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.bool",
			Value:       "true",
			SettingType: models.SystemSettingTypeBool,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})

	t.Run("valid bool value - false", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.bool",
			Value:       "false",
			SettingType: models.SystemSettingTypeBool,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})

	t.Run("valid bool value - 1", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.bool",
			Value:       "1",
			SettingType: models.SystemSettingTypeBool,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})

	t.Run("invalid bool value", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.bool",
			Value:       "not-a-bool",
			SettingType: models.SystemSettingTypeBool,
		}
		err := service.validateValue(setting)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid boolean")
	})

	t.Run("valid JSON value", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.json",
			Value:       `{"key": "value", "num": 123}`,
			SettingType: models.SystemSettingTypeJSON,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})

	t.Run("valid JSON array value", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.json",
			Value:       `["a", "b", "c"]`,
			SettingType: models.SystemSettingTypeJSON,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})

	t.Run("invalid JSON value", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.json",
			Value:       "not-valid-json",
			SettingType: models.SystemSettingTypeJSON,
		}
		err := service.validateValue(setting)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not valid JSON")
	})

	t.Run("string value - any value is valid", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.string",
			Value:       "any value is fine",
			SettingType: models.SystemSettingTypeString,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})

	t.Run("empty string is valid for string type", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.string",
			Value:       "",
			SettingType: models.SystemSettingTypeString,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})
}

func TestDefaultSystemSettings(t *testing.T) {
	defaults := models.DefaultSystemSettings()

	t.Run("returns expected number of defaults", func(t *testing.T) {
		assert.Greater(t, len(defaults), 0)
	})

	t.Run("all defaults have valid types", func(t *testing.T) {
		validTypes := map[string]bool{
			models.SystemSettingTypeString: true,
			models.SystemSettingTypeInt:    true,
			models.SystemSettingTypeBool:   true,
			models.SystemSettingTypeJSON:   true,
		}

		for _, setting := range defaults {
			assert.True(t, validTypes[setting.SettingType],
				"Setting %s has invalid type: %s", setting.SettingKey, setting.SettingType)
		}
	})

	t.Run("all defaults have descriptions", func(t *testing.T) {
		for _, setting := range defaults {
			assert.NotNil(t, setting.Description,
				"Setting %s should have a description", setting.SettingKey)
		}
	})

	t.Run("contains expected settings", func(t *testing.T) {
		expectedKeys := []string{
			"rate_limit.requests_per_minute",
			"rate_limit.requests_per_hour",
			"session.timeout_minutes",
			"websocket.max_participants",
			"features.saml_enabled",
			"features.webhooks_enabled",
			"ui.default_theme",
			"upload.max_file_size_mb",
		}

		defaultKeys := make(map[string]bool)
		for _, setting := range defaults {
			defaultKeys[setting.SettingKey] = true
		}

		for _, key := range expectedKeys {
			assert.True(t, defaultKeys[key], "Expected default setting: %s", key)
		}
	})

	t.Run("default values are valid for their types", func(t *testing.T) {
		service := &SettingsService{}
		for _, setting := range defaults {
			err := service.validateValue(&setting)
			assert.NoError(t, err,
				"Default setting %s has invalid value %s for type %s",
				setting.SettingKey, setting.Value, setting.SettingType)
		}
	})
}

func TestSettingsService_CacheSelection(t *testing.T) {
	t.Run("uses memory cache when Redis is nil", func(t *testing.T) {
		service := NewSettingsService(nil, nil)
		assert.True(t, service.useMemCache)
	})
}

// Note: MockConfigProvider is defined in config_handlers_test.go

func TestSettingsService_ConfigPriority(t *testing.T) {
	t.Run("GetString returns config value over database", func(t *testing.T) {
		service := &SettingsService{
			memCache:            make(map[string]settingsCacheEntry),
			memCacheTTL:         60 * time.Second,
			useMemCache:         true,
			configSettingsCache: make(map[string]MigratableSetting),
		}

		// Set up config provider with a value
		mockProvider := &MockConfigProvider{
			settings: []MigratableSetting{
				{Key: "test.key", Value: "config-value", Type: "string"},
			},
		}
		service.SetConfigProvider(mockProvider)

		// Add a different value to the memory cache (simulating database)
		service.setInMemCache(&models.SystemSetting{
			SettingKey:  "test.key",
			Value:       "database-value",
			SettingType: models.SystemSettingTypeString,
			ModifiedAt:  time.Now(),
		})

		// GetString should return config value, not database value
		val, err := service.GetString(context.Background(), "test.key")
		assert.NoError(t, err)
		assert.Equal(t, "config-value", val)
	})

	t.Run("GetInt returns config value over database", func(t *testing.T) {
		service := &SettingsService{
			memCache:            make(map[string]settingsCacheEntry),
			memCacheTTL:         60 * time.Second,
			useMemCache:         true,
			configSettingsCache: make(map[string]MigratableSetting),
		}

		mockProvider := &MockConfigProvider{
			settings: []MigratableSetting{
				{Key: "test.int", Value: "42", Type: "int"},
			},
		}
		service.SetConfigProvider(mockProvider)

		// Add different value to cache
		service.setInMemCache(&models.SystemSetting{
			SettingKey:  "test.int",
			Value:       "100",
			SettingType: models.SystemSettingTypeInt,
			ModifiedAt:  time.Now(),
		})

		val, err := service.GetInt(context.Background(), "test.int")
		assert.NoError(t, err)
		assert.Equal(t, 42, val) // Should get config value, not 100
	})

	t.Run("GetBool returns config value over database", func(t *testing.T) {
		service := &SettingsService{
			memCache:            make(map[string]settingsCacheEntry),
			memCacheTTL:         60 * time.Second,
			useMemCache:         true,
			configSettingsCache: make(map[string]MigratableSetting),
		}

		mockProvider := &MockConfigProvider{
			settings: []MigratableSetting{
				{Key: "test.bool", Value: "true", Type: "bool"},
			},
		}
		service.SetConfigProvider(mockProvider)

		// Add different value to cache
		service.setInMemCache(&models.SystemSetting{
			SettingKey:  "test.bool",
			Value:       "false",
			SettingType: models.SystemSettingTypeBool,
			ModifiedAt:  time.Now(),
		})

		val, err := service.GetBool(context.Background(), "test.bool")
		assert.NoError(t, err)
		assert.True(t, val) // Should get config value (true), not false
	})

	t.Run("falls back to database when config has no value", func(t *testing.T) {
		service := &SettingsService{
			memCache:            make(map[string]settingsCacheEntry),
			memCacheTTL:         60 * time.Second,
			useMemCache:         true,
			configSettingsCache: make(map[string]MigratableSetting),
		}

		// Empty config provider
		mockProvider := &MockConfigProvider{
			settings: []MigratableSetting{},
		}
		service.SetConfigProvider(mockProvider)

		// Add value to cache (simulating database)
		service.setInMemCache(&models.SystemSetting{
			SettingKey:  "test.key",
			Value:       "database-value",
			SettingType: models.SystemSettingTypeString,
			ModifiedAt:  time.Now(),
		})

		// Should fall back to database value since config doesn't have it
		val, err := service.GetString(context.Background(), "test.key")
		assert.NoError(t, err)
		assert.Equal(t, "database-value", val)
	})

	t.Run("works without config provider", func(t *testing.T) {
		service := &SettingsService{
			memCache:            make(map[string]settingsCacheEntry),
			memCacheTTL:         60 * time.Second,
			useMemCache:         true,
			configSettingsCache: make(map[string]MigratableSetting),
		}
		// No config provider set

		// Add value to cache (simulating database)
		service.setInMemCache(&models.SystemSetting{
			SettingKey:  "test.key",
			Value:       "database-value",
			SettingType: models.SystemSettingTypeString,
			ModifiedAt:  time.Now(),
		})

		// Should get database value when no config provider
		val, err := service.GetString(context.Background(), "test.key")
		assert.NoError(t, err)
		assert.Equal(t, "database-value", val)
	})
}

func TestSettingsService_InvalidateAll(t *testing.T) {
	service := &SettingsService{
		memCache:    make(map[string]settingsCacheEntry),
		memCacheTTL: 60 * time.Second,
		useMemCache: true,
	}

	// Add some entries
	for i := 0; i < 5; i++ {
		setting := &models.SystemSetting{
			SettingKey:  "key." + string(rune('a'+i)),
			Value:       "value",
			SettingType: models.SystemSettingTypeString,
			ModifiedAt:  time.Now(),
		}
		service.setInMemCache(setting)
	}

	// Verify entries exist
	assert.Equal(t, 5, len(service.memCache))

	// Invalidate all
	service.InvalidateAll(context.Background())

	// Verify all cleared
	assert.Equal(t, 0, len(service.memCache))
}

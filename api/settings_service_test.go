package api

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupSettingsTestDB creates an in-memory SQLite DB with just the
// system_settings table, for tests that need a real SettingsService write
// path (SeedDefaults, Set, ReEncryptAll) rather than the nil-gormDB gate
// tests above.
func setupSettingsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.SystemSetting{}))
	return gormDB
}

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
		assert.Equal(t, "test.key", string(cached.SettingKey))
		assert.Equal(t, models.DBText("test-value"), cached.Value)
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

	t.Run("empty string is rejected for string type", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.string",
			Value:       "",
			SettingType: models.SystemSettingTypeString,
		}
		err := service.validateValue(setting)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty value not allowed for string setting")
		assert.Contains(t, err.Error(), "use DELETE")
	})

	t.Run("non-empty string value is valid for string type", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.string",
			Value:       "hello",
			SettingType: models.SystemSettingTypeString,
		}
		err := service.validateValue(setting)
		assert.NoError(t, err)
	})

	t.Run("whitespace-only string value is valid for string type", func(t *testing.T) {
		setting := &models.SystemSetting{
			SettingKey:  "test.string",
			Value:       " ",
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
			assert.True(t, validTypes[string(setting.SettingType)],
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
			// rate_limit.requests_per_minute / _per_hour were retired by
			// #813 -- seeded but read by nothing.
			"session.timeout_minutes",
			"websocket.max_participants",
			"features.saml_enabled",
			"features.webhooks_enabled",
			"ui.default_theme",
			"upload.max_file_size_mb",
		}

		defaultKeys := make(map[string]bool)
		for _, setting := range defaults {
			defaultKeys[string(setting.SettingKey)] = true
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

// The stub settings below carry Explicit: true because they stand for values
// an operator actually supplied — which is the premise of "config priority".
// Config only outranks the database for explicitly-configured operational
// values; a bare struct default must yield, or the settings database is
// unreachable (#415). See settings_precedence_test.go for that half.
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
				{Key: "test.key", Value: "config-value", Type: "string", Source: "config", Explicit: true},
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
				{Key: "test.int", Value: "42", Type: "int", Source: "config", Explicit: true},
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
				{Key: "test.bool", Value: "true", Type: "bool", Source: "config", Explicit: true},
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
	for i := range 5 {
		setting := &models.SystemSetting{
			SettingKey:  models.DBVarchar("key." + string(rune('a'+i))),
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

func TestSettingsService_ListByPrefix(t *testing.T) {
	service := &SettingsService{
		memCache:    make(map[string]settingsCacheEntry),
		memCacheTTL: 60 * time.Second,
		useMemCache: true,
	}

	t.Run("returns empty slice when no matches", func(t *testing.T) {
		result, err := service.ListByPrefix(context.Background(), "auth.oauth.providers.")
		assert.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestSettingsService_RefusesBootstrapKeyFromDB(t *testing.T) {
	// A CategoryBootstrap key must never be served from the DB path.
	svc := NewSettingsService(nil, nil) // no DB, no Redis — exercises the guard
	_, err := svc.Get(context.Background(), "database.url")
	if err == nil {
		t.Fatal("Get(database.url) should refuse: bootstrap keys are not DB-served")
	}
	assert.Contains(t, err.Error(), "bootstrap key",
		"the bootstrap guard error should identify the key as a bootstrap key")
}

func TestSettingsService_BootstrapGuardScope(t *testing.T) {
	// The bootstrap guard must fire ONLY for bootstrap keys. For a
	// non-bootstrap key the guard must NOT short-circuit Get: the call should
	// proceed to the DB path and fail there (nil DB) with a different error.
	svc := NewSettingsService(nil, nil) // no DB, no Redis

	t.Run("guard does not fire for an unknown (non-bootstrap) key", func(t *testing.T) {
		defer func() {
			// With no guard match and a nil DB, the DB path panics on the
			// nil *gorm.DB. Recovering proves the guard did NOT short-circuit
			// the call — i.e. the key was not treated as a bootstrap key.
			if r := recover(); r == nil {
				t.Fatal("expected the non-bootstrap key to reach the DB path (nil-DB panic)")
			}
		}()
		_, err := svc.Get(context.Background(), "some.unknown.key")
		// If Get returns instead of panicking, the error must not be the
		// bootstrap-guard error.
		if err != nil {
			assert.NotContains(t, err.Error(), "bootstrap key",
				"a non-bootstrap key must not trigger the bootstrap guard")
		}
	})

	t.Run("guard does not fire for an operational key", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected the operational key to reach the DB path (nil-DB panic)")
			}
		}()
		_, err := svc.Get(context.Background(), "websocket.inactivity_timeout_seconds")
		if err != nil {
			assert.NotContains(t, err.Error(), "bootstrap key",
				"an operational key must not trigger the bootstrap guard")
		}
	})
}

func TestSettingsService_SetRefusesPlaintextSecretInProduction(t *testing.T) {
	t.Setenv("TMI_BUILD_MODE", "production")

	// No DB, no Redis, no encryptor. If the gate fails to short-circuit, the
	// write path panics on the nil *gorm.DB — so a returned error proves the
	// row was never written.
	svc := NewSettingsService(nil, nil)

	setting := &models.SystemSetting{
		SettingKey:  "timmy.llm_api_key", // operational, Secret:true in the classification registry
		Value:       "sk-super-secret",
		SettingType: models.SystemSettingTypeString,
	}
	err := svc.Set(context.Background(), setting)
	require.Error(t, err, "Set of a secret-classified key with no encryptor must fail in production")
	assert.Contains(t, err.Error(), "secret-classified")

	t.Run("disabled passthrough encryptor is also rejected", func(t *testing.T) {
		svc := NewSettingsService(nil, nil)
		svc.SetEncryptor(&crypto.SettingsEncryptor{}) // zero value: IsEnabled() == false
		err := svc.Set(context.Background(), setting)
		require.Error(t, err, "a non-nil but disabled encryptor must not satisfy the gate")
		assert.Contains(t, err.Error(), "secret-classified")
	})

	t.Run("provider secret sub-keys are covered", func(t *testing.T) {
		for _, key := range []string{
			"auth.oauth.providers.google.client_secret",
			"auth.saml.providers.okta.sp_private_key",
			"content_oauth.providers.github.client_secret",
		} {
			svc := NewSettingsService(nil, nil)
			err := svc.Set(context.Background(), &models.SystemSetting{
				SettingKey:  models.DBVarchar(key),
				Value:       "hunter2",
				SettingType: models.SystemSettingTypeString,
			})
			require.Error(t, err, "Set(%s) must fail: provider secrets must not be stored plaintext", key)
			assert.Contains(t, err.Error(), "secret-classified")
		}
	})

	t.Run("non-secret keys still reach the write path", func(t *testing.T) {
		defer func() {
			// With the gate not firing and a nil DB, the write path panics on
			// the nil *gorm.DB. Recovering proves the gate did NOT block a
			// non-secret key in production.
			if r := recover(); r == nil {
				t.Fatal("expected the non-secret key to reach the DB write path (nil-DB panic)")
			}
		}()
		svc := NewSettingsService(nil, nil)
		_ = svc.Set(context.Background(), &models.SystemSetting{
			SettingKey:  "websocket.inactivity_timeout_seconds",
			Value:       "300",
			SettingType: models.SystemSettingTypeInt,
		})
	})
}

func TestSettingsService_SetAllowsPlaintextSecretOutsideProduction(t *testing.T) {
	t.Setenv("TMI_BUILD_MODE", "dev")

	// In dev the gate warns but allows: the write proceeds to the nil *gorm.DB
	// and panics. Recovering proves the gate did not block.
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected the dev-mode write to reach the DB write path (nil-DB panic)")
		}
	}()
	svc := NewSettingsService(nil, nil)
	_ = svc.Set(context.Background(), &models.SystemSetting{
		SettingKey:  "timmy.llm_api_key",
		Value:       "sk-dev-secret",
		SettingType: models.SystemSettingTypeString,
	})
}

// TestSettingsService_SeedDefaults_StampsSeededOrigin pins #794: every row
// SeedDefaults inserts — both the explicit models.DefaultSystemSettings()
// entries and the config-derived operational defaults it appends — must be
// stamped SystemSettingOriginSeeded, since no operator has set them.
func TestSettingsService_SeedDefaults_StampsSeededOrigin(t *testing.T) {
	gormDB := setupSettingsTestDB(t)
	svc := NewSettingsService(gormDB, nil)

	require.NoError(t, svc.SeedDefaults(context.Background()))

	var settings []models.SystemSetting
	require.NoError(t, gormDB.Find(&settings).Error)
	require.Greater(t, len(settings), len(models.DefaultSystemSettings()),
		"expected config-derived operational defaults to be seeded alongside the explicit defaults")

	for _, s := range settings {
		assert.Truef(t, s.Origin.Valid && s.Origin.String == models.SystemSettingOriginSeeded,
			"setting %s: expected origin %q, got valid=%v value=%q",
			s.SettingKey, models.SystemSettingOriginSeeded, s.Origin.Valid, s.Origin.String)
		assert.Falsef(t, s.IsExplicit(), "setting %s: seeded row must not be IsExplicit()", s.SettingKey)
	}

	// Spot-check one of the explicit models.DefaultSystemSettings() entries
	// specifically, since that's the other of the two source lists.
	var explicit models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "ui.default_theme").First(&explicit).Error)
	assert.Equal(t, models.SystemSettingOriginSeeded, explicit.Origin.String)
}

// TestSettingsService_Set_StampsExplicitOrigin pins #794: Set always means an
// operator deliberately wrote this value, so it must stamp explicit origin
// regardless of what the caller's struct carried in, and it must reflect that
// back onto the caller's setting the same way it already does for ModifiedAt.
func TestSettingsService_Set_StampsExplicitOrigin(t *testing.T) {
	gormDB := setupSettingsTestDB(t)
	svc := NewSettingsService(gormDB, nil)

	setting := &models.SystemSetting{
		SettingKey:  "ui.default_theme",
		Value:       "dark",
		SettingType: models.SystemSettingTypeString,
	}
	require.NoError(t, svc.Set(context.Background(), setting))

	assert.True(t, setting.Origin.Valid && setting.Origin.String == models.SystemSettingOriginExplicit,
		"Set should reflect explicit origin back onto the caller's setting")

	var stored models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "ui.default_theme").First(&stored).Error)
	assert.Equal(t, models.SystemSettingOriginExplicit, stored.Origin.String)
	assert.True(t, stored.IsExplicit())

	t.Run("Set promotes an already-seeded row to explicit", func(t *testing.T) {
		seeded := models.SystemSetting{
			SettingKey:  "session.timeout_minutes",
			Value:       "60",
			SettingType: models.SystemSettingTypeInt,
			Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginSeeded, Valid: true},
		}
		require.NoError(t, gormDB.Create(&seeded).Error)

		update := &models.SystemSetting{
			SettingKey:  "session.timeout_minutes",
			Value:       "90",
			SettingType: models.SystemSettingTypeInt,
		}
		require.NoError(t, svc.Set(context.Background(), update))

		var stored models.SystemSetting
		require.NoError(t, gormDB.Where("setting_key = ?", "session.timeout_minutes").First(&stored).Error)
		assert.Equal(t, models.SystemSettingOriginExplicit, stored.Origin.String,
			"operator overwriting a seeded row via Set must promote it to explicit")
	})
}

// TestSettingsService_ReEncryptAll_PreservesOrigin is the #794/#805
// regression guard: ReEncryptAll is a mechanical key-rotation pass, not an
// operator edit, so it must write only the ciphertext. It must NOT touch
// Origin (if key rotation promoted every seeded row to explicit, every
// operational key would suddenly out-rank config) and it must NOT touch
// modified_by or modified_at (the #794 origin backfill reads modified_by as
// operator intent, and a nil actor used to clear it to NULL).
// SEM@9a4d6109d4ad52d5adc53c0fe0d9925022535958: verify re-encryption rotates ciphertext but leaves origin and audit fields untouched
func TestSettingsService_ReEncryptAll_PreservesOrigin(t *testing.T) {
	gormDB := setupSettingsTestDB(t)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	enc, err := crypto.NewSettingsEncryptorFromKeys(key, nil, 1)
	require.NoError(t, err)

	svc := NewSettingsService(gormDB, nil)
	svc.SetEncryptor(enc)

	// A fixed past timestamp: autoUpdateTime only fills modified_at on Create
	// when it is zero, so this value is what lands in the row.
	stampedAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	operator := "11111111-2222-3333-4444-555555555555"

	seeded := models.SystemSetting{
		SettingKey:  "session.timeout_minutes",
		Value:       "60", // plaintext; ReEncryptAll must accept this as well as already-encrypted values
		SettingType: models.SystemSettingTypeInt,
		ModifiedAt:  stampedAt,
		// ModifiedBy deliberately NULL: nobody ever set this row.
		Origin: models.NullableDBVarchar{String: models.SystemSettingOriginSeeded, Valid: true},
	}
	require.NoError(t, gormDB.Create(&seeded).Error)

	explicit := models.SystemSetting{
		SettingKey:  "ui.default_theme",
		Value:       "dark",
		SettingType: models.SystemSettingTypeString,
		ModifiedAt:  stampedAt,
		ModifiedBy:  models.NewNullableDBVarchar(&operator),
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
	}
	require.NoError(t, gormDB.Create(&explicit).Error)

	count, settingErrors, err := svc.ReEncryptAll(context.Background())
	require.NoError(t, err)
	assert.Empty(t, settingErrors)
	assert.Equal(t, 2, count)

	var reloadedSeeded models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "session.timeout_minutes").First(&reloadedSeeded).Error)
	assert.Equal(t, models.SystemSettingOriginSeeded, reloadedSeeded.Origin.String,
		"ReEncryptAll must not promote a seeded row to explicit")
	assert.False(t, reloadedSeeded.IsExplicit())
	assert.False(t, reloadedSeeded.ModifiedBy.Valid,
		"ReEncryptAll must leave a NULL modified_by NULL")
	assert.True(t, stampedAt.Equal(reloadedSeeded.ModifiedAt),
		"ReEncryptAll must not move modified_at (got %v)", reloadedSeeded.ModifiedAt)
	assert.NotEqual(t, "60", string(reloadedSeeded.Value), "value must now be ciphertext")
	plain, err := enc.Decrypt(string(reloadedSeeded.Value))
	require.NoError(t, err)
	assert.Equal(t, "60", plain)

	var reloadedExplicit models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "ui.default_theme").First(&reloadedExplicit).Error)
	assert.Equal(t, models.SystemSettingOriginExplicit, reloadedExplicit.Origin.String)
	assert.True(t, reloadedExplicit.IsExplicit())
	require.True(t, reloadedExplicit.ModifiedBy.Valid, "ReEncryptAll must not clear modified_by")
	assert.Equal(t, operator, reloadedExplicit.ModifiedBy.String,
		"ReEncryptAll must not overwrite modified_by")
	assert.True(t, stampedAt.Equal(reloadedExplicit.ModifiedAt),
		"ReEncryptAll must not move modified_at (got %v)", reloadedExplicit.ModifiedAt)
	assert.NotEqual(t, "dark", string(reloadedExplicit.Value), "value must now be ciphertext")
	plain, err = enc.Decrypt(string(reloadedExplicit.Value))
	require.NoError(t, err)
	assert.Equal(t, "dark", plain)
}

// TestSettingsService_ReEncryptAll_ReportsVanishedRow is the #805
// oracle-db-admin follow-up: a row deleted between ReEncryptAll's initial
// Find and its per-row UpdateColumn must be reported as a SettingError, not
// counted as re-encrypted. A GORM "before update" callback deletes the row
// out from under the write to simulate that race deterministically.
// SEM@5740a75fafc8da46a061901361ed61990a6c8916: verify re-encryption reports a concurrently deleted row instead of counting it
func TestSettingsService_ReEncryptAll_ReportsVanishedRow(t *testing.T) {
	gormDB := setupSettingsTestDB(t)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	enc, err := crypto.NewSettingsEncryptorFromKeys(key, nil, 1)
	require.NoError(t, err)

	svc := NewSettingsService(gormDB, nil)
	svc.SetEncryptor(enc)

	require.NoError(t, gormDB.Create(&models.SystemSetting{
		SettingKey:  "will.vanish",
		Value:       "60",
		SettingType: models.SystemSettingTypeInt,
	}).Error)
	require.NoError(t, gormDB.Create(&models.SystemSetting{
		SettingKey:  "will.survive",
		Value:       "dark",
		SettingType: models.SystemSettingTypeString,
	}).Error)

	// Delete "will.vanish" the moment any row's UpdateColumn is about to
	// run, regardless of which row it is: whichever of the two updates
	// fires first, "will.vanish" is gone from under its own write by the
	// time that write executes. Reuses the pending statement's own
	// session/connection (NewDB: true clones the session without carrying
	// over the pending update's Statement) so this doesn't contend with it
	// for a second SQLite ":memory:" connection, which would otherwise be a
	// distinct, schema-less database.
	require.NoError(t, gormDB.Callback().Update().Before("gorm:update").
		Register("test:vanish_row", func(tx *gorm.DB) {
			tx.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).
				Exec("DELETE FROM system_settings WHERE setting_key = ?", "will.vanish")
		}))

	count, settingErrors, err := svc.ReEncryptAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count, "the surviving row is still counted")
	require.Len(t, settingErrors, 1)
	assert.Equal(t, "will.vanish", settingErrors[0].Key)
	assert.Equal(t, "setting no longer exists", settingErrors[0].Error)

	var reloadedSurvivor models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "will.survive").First(&reloadedSurvivor).Error)
	plain, err := enc.Decrypt(string(reloadedSurvivor.Value))
	require.NoError(t, err)
	assert.Equal(t, "dark", plain)
}

// A missing row is negative-cached so unauthenticated hot paths do not pay a
// DB round trip per request for keys that are never seeded (#770).
// SEM@42f901dab9ff2a3942068435a791d370c03c8f6b: verify a missing setting is negative-cached in memory until invalidated
func TestSettingsService_NegativeCache(t *testing.T) {
	ctx := context.Background()
	gormDB := setupSettingsTestDB(t)
	service := &SettingsService{
		gormDB:      gormDB,
		memCache:    make(map[string]settingsCacheEntry),
		memCacheTTL: 60 * time.Second,
		useMemCache: true,
	}
	const key = "auth.oauth.client_callback_allowlist"

	setting, err := service.Get(ctx, key)
	require.NoError(t, err)
	require.Nil(t, setting)
	_, found := service.getFromMemCache(key)
	require.True(t, found, "miss should leave a tombstone in the cache")

	// A row that appears behind the tombstone is invisible until the key is
	// invalidated: proves the second Get never touched the database.
	require.NoError(t, gormDB.Create(&models.SystemSetting{
		SettingKey: key, Value: `["http://a/"]`, SettingType: models.SystemSettingTypeJSON,
	}).Error)
	setting, err = service.Get(ctx, key)
	require.NoError(t, err)
	require.Nil(t, setting, "tombstone must be served without a DB read")

	service.invalidateCache(ctx, key)
	setting, err = service.Get(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, setting)
	require.Equal(t, `["http://a/"]`, string(setting.Value))
}

// Same contract on the Redis tier, which is what production runs: the
// tombstone is the literal "null", which unmarshals to a zero SystemSetting.
// SEM@60ebb8f27ed8310f384bb5c763ccff24a48e2cff: verify a missing setting is negative-cached in Redis until invalidated
func TestSettingsService_NegativeCache_Redis(t *testing.T) {
	ctx := context.Background()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	host, port, err := net.SplitHostPort(mr.Addr())
	require.NoError(t, err)
	redisDB, err := db.NewRedisDB(db.RedisConfig{Host: host, Port: port})
	require.NoError(t, err)
	defer func() { _ = redisDB.Close() }()

	gormDB := setupSettingsTestDB(t)
	service := &SettingsService{gormDB: gormDB, redis: redisDB}
	const key = "auth.oauth.client_callback_allowlist"

	setting, err := service.Get(ctx, key)
	require.NoError(t, err)
	require.Nil(t, setting)
	require.True(t, mr.Exists(SettingsCacheKey+key), "miss should leave a tombstone in Redis")

	require.NoError(t, gormDB.Create(&models.SystemSetting{
		SettingKey: key, Value: `["http://a/"]`, SettingType: models.SystemSettingTypeJSON,
	}).Error)
	setting, err = service.Get(ctx, key)
	require.NoError(t, err)
	require.Nil(t, setting, "tombstone must be served without a DB read")

	service.invalidateCache(ctx, key)
	setting, err = service.Get(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, setting)
	require.Equal(t, `["http://a/"]`, string(setting.Value))
}

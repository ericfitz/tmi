package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettingsService provides access to system settings with caching.
//
// Configuration Priority:
// Every read path (GetString, GetInt, GetBool, GetJSON, GetResolvedString and
// the admin settings reads) applies one rule, implemented by configOutranksRow:
// an explicit (admin-set or dbtool-imported) database row wins over config,
// explicit or not; a seeded default row yields to explicit config (an env var
// or a key written in the config file); with no usable row (missing, empty,
// unreadable, or failing its per-key semantic validator) the config layer's
// value, explicit or default, is used.
// Bootstrap settings are never database-backed, so config is their only source.
//
// The ConfigProvider (via GetMigratableSettings) already merges environment
// variables with config file values (environment takes precedence). The
// database holds runtime-editable values set through the admin API (#1168).
type SettingsService struct {
	gormDB  *gorm.DB
	redis   *db.RedisDB
	builder *db.RedisKeyBuilder

	// In-memory cache for when Redis is unavailable
	memCache    map[string]settingsCacheEntry
	memCacheMu  sync.RWMutex
	memCacheTTL time.Duration
	useMemCache bool

	// Config provider for environment/config file values (see the precedence rule above)
	configProvider ConfigProvider
	// Cached config settings map for fast lookups
	configSettingsCache    map[string]MigratableSetting
	configSettingsCacheMu  sync.RWMutex
	configSettingsCacheSet bool

	// Encryptor for at-rest encryption of setting values
	encryptor *crypto.SettingsEncryptor

	// warnedInvalidRows records keys whose invalid row has been logged (#1181).
	warnedInvalidRows sync.Map
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

// SetConfigProvider sets the config provider consulted when resolving a setting
// (environment and config-file values). Resolution follows the row-wins rule
// (#1168): an admin-set database row outranks config, and config outranks a
// seeded row only when it is explicit. Bootstrap keys never read the database.
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

// isProductionMode reports whether the server is running a production build.
// It prefers the resolved auth.build_mode value from the config provider (the
// same cfg.Auth.BuildMode that warnIfPlaintextSecretsAtRest consults) and
// falls back to the TMI_BUILD_MODE environment variable, the existing idiom in
// this package (see auth_flow_rate_limiter.go).
func (s *SettingsService) isProductionMode() bool {
	// auth.build_mode is a bootstrap key: config is its only source.
	if ms, ok := s.getConfigSettingRaw("auth.build_mode"); ok {
		return ms.Value == "production"
	}
	return os.Getenv("TMI_BUILD_MODE") == "production"
}

// getConfigSettingRaw retrieves a setting from the config provider's cache
// without applying any precedence rule, building the cache lazily. The caller
// decides what to do with Class.Category and Explicit.
//
// The precedence rule needs to know both what the config layer holds AND
// whether an operator supplied it, and those are two different questions
// (#794); see resolveSetting.
func (s *SettingsService) getConfigSettingRaw(key string) (MigratableSetting, bool) {
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
	if !found {
		return MigratableSetting{}, false
	}
	return setting, true
}

// Get retrieves a setting by key, checking cache first
func (s *SettingsService) Get(ctx context.Context, key string) (*models.SystemSetting, error) {
	logger := slogging.Get()

	// Bootstrap settings are file/env only and must never be served from the
	// database. classificationFor returns CategoryBootstrap for them.
	if config.ClassificationCategoryFor(key) == config.CategoryBootstrap {
		return nil, fmt.Errorf("setting %q is a bootstrap key: read it from config/env, not the database", key)
	}

	// Try cache first
	setting, found := s.getFromCache(ctx, key)
	if found {
		logger.Debug("Settings cache hit for key: %s", key)
		if setting.SettingKey == "" {
			return nil, nil // negative-cache tombstone: known missing (#770)
		}
		return setting, nil
	}
	logger.Debug("Settings cache miss for key: %s", key)

	// Load from database
	var dbSetting models.SystemSetting
	if err := s.gormDB.WithContext(ctx).Where("setting_key = ?", key).First(&dbSetting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Cache the miss too: keys that are never seeded (e.g. an
			// allowlist managed purely via env) sit on unauthenticated hot
			// paths and would otherwise cost an ADB round trip per request.
			// Set/Delete invalidate the key, which evicts the tombstone (#770).
			s.setMissingInCache(ctx, key)
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("failed to get setting %s: %w", key, err)
	}

	// Decrypt if encryptor is configured
	if s.encryptor != nil {
		decrypted, err := s.encryptor.Decrypt(string(dbSetting.Value))
		if err != nil {
			logger.Warn("Setting %s is unreadable: stored value could not be decrypted", key)
			// Return the row's non-secret metadata (type, origin, timestamps)
			// with the error so the admin API can describe it; Value is
			// cleared so ciphertext never leaks. Callers must check err first.
			dbSetting.Value = ""
			dbSetting.Unreadable = true
			return &dbSetting, fmt.Errorf("%w: setting %s could not be decrypted: %w", errSettingUnreadable, key, err)
		}
		dbSetting.Value = models.DBText(decrypted)
	}

	// Cache the decrypted result (cache stores plaintext)
	s.setInCache(ctx, &dbSetting)

	return &dbSetting, nil
}

// getUsableRow is Get with "unreadable row == no usable row" applied (#1169):
// a row that cannot be decrypted is reported as absent (nil, nil) rather than
// as an error, so every precedence-resolving reader falls through to the
// config layer instead of failing. Get has already logged the key. Callers that
// must distinguish the two (the admin API) call Get and test for
// errSettingUnreadable.
func (s *SettingsService) getUsableRow(ctx context.Context, key string) (*models.SystemSetting, error) {
	row, err := s.Get(ctx, key)
	if errors.Is(err, errSettingUnreadable) {
		return nil, nil
	}
	return row, err
}

// resolveSetting applies the single precedence rule (configOutranksRow) to a
// scalar setting and returns the winning raw string. found is false when no
// layer supplies a usable value. fromConfig reports which layer won, so callers
// can name it in parse errors. A non-explicit config default that is empty is
// treated as unset, so typed getters return the zero value rather than a parse
// error. Bootstrap keys resolve from config only and never touch the database.
func (s *SettingsService) resolveSetting(ctx context.Context, key string) (value string, found, fromConfig bool, err error) {
	cfg, cfgFound := s.getConfigSettingRaw(key)

	if config.ClassificationCategoryFor(key) == config.CategoryBootstrap ||
		(cfgFound && cfg.Class.Category == config.CategoryBootstrap) {
		return cfg.Value, cfgFound, true, nil
	}

	row, err := s.getUsableRow(ctx, key)
	if err != nil {
		// Before #1168 explicit config never touched the database, so a
		// transient fault (ORA-03113/12537 class) must not make a value the
		// config layer can answer unreadable: fall back to config, unretried
		// because /config reads many keys back to back. With nothing in config
		// the classified error is all there is.
		classified := dberrors.Classify(err)
		if errors.Is(classified, dberrors.ErrTransient) && cfgFound {
			slogging.Get().Warn("Transient database error reading setting %s; using the config layer value", key)
			row = nil
		} else {
			return "", false, false, classified
		}
	}
	if s.configOutranksRow(key, cfgFound && cfg.Explicit, row) {
		if !cfgFound || (cfg.Value == "" && !cfg.Explicit) {
			return "", false, true, nil
		}
		return cfg.Value, true, true, nil
	}
	return string(row.Value), true, false, nil
}

// GetString retrieves a string setting value.
// Priority: see the SettingsService documentation (admin-set row wins).
func (s *SettingsService) GetString(ctx context.Context, key string) (string, error) {
	v, _, _, err := s.resolveSetting(ctx, key)
	return v, err
}

// GetResolvedString retrieves a string setting by applying the single
// precedence rule that governs every scalar operational setting (#794).
// exists reports whether any layer supplied a usable value, so callers can
// distinguish "nothing configured anywhere" from an empty value.
//
// This replaced GetDatabaseString, which read the database only. Two
// accessors used to implement two deliberate, opposite rules — GetString
// (#415: an explicit config value wins, a bare struct default yields to the
// database) and GetDatabaseString (#767: the database always wins, config is
// a first-run fallback). Each was right about half the problem, and the half
// each got wrong caused a production outage on 2026-08-20: a registry-seeded
// default row nobody had ever set outranked a correctly-set
// TMI_OAUTH_CALLBACK_URL, so every OAuth login redirected to localhost.
//
// The missing distinction was on the database side. #415 already knew an
// operator-supplied config value from a struct default (MigratableSetting
// .Explicit); the database had no equivalent until SystemSetting.Origin, so a
// seeded row and an admin-set row were indistinguishable. With both halves
// present, one rule covers every case:
//
//	config explicit?  db explicit?  winner
//	no                no            database  (both defaults; prefer the hot-reloadable one)
//	no                yes           database  (#415 preserved)
//	yes               no            config    (a seeded default never beats an operator)
//	yes               yes           database  (#419/#767 runtime editability preserved)
//
// Bootstrap settings are exempt: they are never database-backed, so the
// config layer is their only source.
//
// The read is retried on transient faults and the returned error is
// classified (dberrors sentinels): callers on the #419 runtime path sit on
// request hot paths (e.g. /oauth2/authorize) where an unretried ADB blip
// (ORA-02396/03113/12537 class) would otherwise surface as a hard failure
// PG testing cannot reproduce (oracle-db-admin review of #767).
func (s *SettingsService) GetResolvedString(ctx context.Context, key string) (string, bool, error) {
	cfg, cfgFound := s.getConfigSettingRaw(key)

	// Bootstrap keys never reach the database at all. Keyed on the
	// classification registry rather than on cfg.Class, so the guard still
	// holds when the config provider is unset or does not emit the key —
	// otherwise a bootstrap key would fall through to Get, whose "is a
	// bootstrap key" error matches no dberrors pattern and would surface to
	// the adapters as a non-transient fault (oracle-db-admin review, note 5).
	// This is the same source Get itself consults.
	if config.ClassificationCategoryFor(key) == config.CategoryBootstrap {
		return cfg.Value, cfg.Value != "", nil
	}

	var row *models.SystemSetting
	err := db.WithRetryableGormRead(ctx, db.DefaultRetryConfig(), func() error {
		var readErr error
		row, readErr = s.getUsableRow(ctx, key)
		if readErr != nil {
			return dberrors.Classify(readErr)
		}
		return nil
	})
	if err != nil {
		return "", false, err
	}

	if s.configOutranksRow(key, cfgFound && cfg.Explicit, row) {
		return cfg.Value, cfgFound && cfg.Value != "", nil
	}
	return string(row.Value), true, nil
}

// configOutranksRow reports whether the config layer's value, rather than the
// database row, is the one in effect for a non-bootstrap setting. It is the
// single implementation of the precedence table on GetResolvedString, shared
// by the runtime read path and the admin settings read endpoints so that the
// admin API reports the value the server actually uses (#1161). row may be
// nil (no row).
func configOutranksRow(cfgExplicit bool, row *models.SystemSetting, productionMode bool) bool {
	switch {
	case row == nil || row.Unreadable || string(row.Value) == "" || rowFailsSemantics(row, productionMode):
		// Nothing usable in the database — the config layer is all there is,
		// explicit or not. An empty row value is treated as absent: on Oracle
		// '' binds as NULL, so an empty-but-present row is indistinguishable
		// from a missing one anyway — collapsing them keeps the rule
		// engine-independent (oracle-db-admin review of #767, note 3). A row
		// that could not be decrypted is likewise no usable row (#1169), and so
		// is one whose value fails its per-key semantic validator (#1181).
		return true
	case row.IsExplicit():
		// An operator deliberately set this row, via the admin API or a
		// dbtool import. It outranks config either way, which is what keeps
		// runtime edits effective without a redeploy (#419/#767).
		return false
	default:
		// The row is a seeded registry default. An explicit operator value
		// beats it (the case that broke production); when both sides are
		// defaults, prefer the row, which an admin can change at runtime.
		return cfgExplicit
	}
}

// rowFailsSemantics reports whether a stored row's value fails the per-key
// semantic validator, which makes it unusable (#1181). It is computed at
// resolution time rather than stored on the row, because cached rows would not
// carry a flag and a validator can tighten in a later release. Keys without a
// validator never fail. The validator's message is deliberately dropped: it
// may echo the value.
func rowFailsSemantics(row *models.SystemSetting, productionMode bool) bool {
	return ValidateSettingSemantics(string(row.SettingKey), string(row.Value), productionMode) != nil
}

// configOutranksRow is the service-side entry to the package-level rule: it
// supplies the build mode and, for an invalid row, logs the key (once per key
// per process, since resolution runs on every read) so the ignored row is
// visible. Values are never logged.
func (s *SettingsService) configOutranksRow(key string, cfgExplicit bool, row *models.SystemSetting) bool {
	prod := s.isProductionMode()
	if row != nil && !row.Unreadable && string(row.Value) != "" && rowFailsSemantics(row, prod) {
		if _, seen := s.warnedInvalidRows.LoadOrStore(key, struct{}{}); !seen {
			slogging.Get().Warn("System setting %s has a stored value that fails validation; ignoring the row and using the config layer value", key)
		}
	}
	return configOutranksRow(cfgExplicit, row, prod)
}

// settingLayer names the winning layer for parse-error messages.
func settingLayer(fromConfig bool) string {
	if fromConfig {
		return "config setting"
	}
	return "setting"
}

// parseCause strips the offending input from a strconv error: *strconv.NumError
// formats as `strconv.Atoi: parsing "<input>": ...`, which would put a stored
// (possibly secret) setting value in the error and the log lines built from it.
func parseCause(err error) error {
	var numErr *strconv.NumError
	if errors.As(err, &numErr) {
		return numErr.Err
	}
	return err
}

// GetInt retrieves an integer setting value.
// Priority: see the SettingsService documentation (admin-set row wins).
func (s *SettingsService) GetInt(ctx context.Context, key string) (int, error) {
	raw, found, fromConfig, err := s.resolveSetting(ctx, key)
	if err != nil || !found {
		return 0, err
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %s is not a valid integer: %w", settingLayer(fromConfig), key, parseCause(err))
	}
	return val, nil
}

// GetBool retrieves a boolean setting value.
// Priority: see the SettingsService documentation (admin-set row wins).
func (s *SettingsService) GetBool(ctx context.Context, key string) (bool, error) {
	raw, found, fromConfig, err := s.resolveSetting(ctx, key)
	if err != nil || !found {
		return false, err
	}
	val, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s %s is not a valid boolean: %w", settingLayer(fromConfig), key, parseCause(err))
	}
	return val, nil
}

// GetJSON retrieves a JSON setting value and unmarshals it into the target.
// Priority: see the SettingsService documentation (admin-set row wins).
func (s *SettingsService) GetJSON(ctx context.Context, key string, target any) error {
	raw, found, fromConfig, err := s.resolveSetting(ctx, key)
	if err != nil || !found {
		return err
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("%s %s is not valid JSON: %w", settingLayer(fromConfig), key, err)
	}
	return nil
}

// List retrieves all settings. A row whose value cannot be decrypted is still
// returned, with Unreadable set and Value empty.
func (s *SettingsService) List(ctx context.Context) ([]models.SystemSetting, error) {
	logger := slogging.Get()

	var settings []models.SystemSetting
	if err := s.gormDB.WithContext(ctx).Order("setting_key").Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}

	// Decrypt all values
	if s.encryptor != nil {
		for i := range settings {
			decrypted, err := s.encryptor.Decrypt(string(settings[i].Value))
			if err != nil {
				// One unreadable row must not fail the whole list (#1169):
				// flag it, drop the ciphertext, and let callers mask it.
				logger.Warn("Setting %s is unreadable: stored value could not be decrypted", settings[i].SettingKey)
				settings[i].Value = ""
				settings[i].Unreadable = true
				continue
			}
			settings[i].Value = models.DBText(decrypted)
		}
	}

	return settings, nil
}

// ListByPrefix returns all database settings whose key starts with the given prefix.
// Results are decrypted if an encryptor is configured.
func (s *SettingsService) ListByPrefix(ctx context.Context, prefix string) ([]models.SystemSetting, error) {
	if s.gormDB == nil {
		return nil, nil
	}

	var settings []models.SystemSetting
	result := s.gormDB.WithContext(ctx).Where("setting_key LIKE ?", prefix+"%").Find(&settings)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to list settings by prefix %q: %w", prefix, result.Error)
	}

	// Decrypt values if encryptor is configured
	if s.encryptor != nil {
		for i := range settings {
			decrypted, err := s.encryptor.Decrypt(string(settings[i].Value))
			if err != nil {
				// Value may not be encrypted (e.g., migrated from before encryption was enabled)
				continue
			}
			settings[i].Value = models.DBText(decrypted)
		}
	}

	return settings, nil
}

// settingSemanticValidators is the single key -> validator map applied by Set,
// so semantic checks on specific settings are enforced for every writer that
// goes through Set (the admin API) rather than per handler. Writers that
// bypass Set (raw DB writes) are not covered here; a stored value that fails
// its validator is ignored at read time instead (#1181). dbtool reuses this map through
// ValidateSettingSemantics so its direct writes obey the same rules (#1108).
// The validators mirror the startup checks in config.ValidateTimmyConfig (#1102).
var settingSemanticValidators = map[string]func(value string, productionMode bool) error{
	"timmy.dump_extracted_text_to_note": func(value string, productionMode bool) error {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("timmy.dump_extracted_text_to_note: value is not a valid boolean")
		}
		buildMode := ""
		if productionMode {
			buildMode = "production"
		}
		return config.ValidateTimmyDumpFlag(enabled, buildMode)
	},
	"timmy.llm_provider": func(value string, _ bool) error {
		return config.TimmyConfig{LLMProvider: value}.ValidateLLMProvider()
	},
	"timmy.llm_max_tokens": func(value string, _ bool) error {
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("timmy.llm_max_tokens: value is not a valid integer")
		}
		return config.TimmyConfig{LLMMaxTokens: n}.ValidateLLMMaxTokens()
	},
}

// ValidateSettingSemantics applies the per-key semantic validator for key, if
// any; keys without a validator pass. Exported so writers that bypass
// SettingsService.Set (cmd/dbtool) enforce the same rules (#1108).
func ValidateSettingSemantics(key, value string, productionMode bool) error {
	validate, ok := settingSemanticValidators[key]
	if !ok {
		return nil
	}
	return validate(value, productionMode)
}

// validateSettingSemantics applies the per-key validator for the setting, if any.
func (s *SettingsService) validateSettingSemantics(setting *models.SystemSetting) error {
	return ValidateSettingSemantics(string(setting.SettingKey), string(setting.Value), s.isProductionMode())
}

// Set creates or updates a setting
func (s *SettingsService) Set(ctx context.Context, setting *models.SystemSetting) error {
	logger := slogging.Get()

	// Validate setting type
	switch setting.SettingType {
	case models.SystemSettingTypeString, models.SystemSettingTypeInt,
		models.SystemSettingTypeBool, models.SystemSettingTypeJSON,
		models.SystemSettingTypeFloat:
		// Valid
	default:
		return fmt.Errorf("invalid setting type: %s", setting.SettingType)
	}

	// Validate value matches type (validate plaintext before encryption)
	if err := s.validateValue(setting); err != nil {
		return err
	}

	// Per-key semantic validation (#1102): reject values the server would
	// refuse to boot with.
	if err := s.validateSettingSemantics(setting); err != nil {
		return err
	}

	// Enforce the secret => encrypted invariant at write time. The Secret
	// classification is not masking-only: the set of keys we refuse to persist
	// in plaintext here is exactly the set shouldMaskSettingValue redacts from
	// API responses. If encryption is unavailable (no encryptor wired, or a
	// disabled passthrough), refuse to persist a secret-classified value in
	// production builds; outside production warn and allow, mirroring the
	// dev/production severity scaling of warnIfPlaintextSecretsAtRest in
	// cmd/server/startup_checks.go.
	encryptionEnabled := s.encryptor != nil && s.encryptor.IsEnabled()
	if !encryptionEnabled && shouldMaskSettingValue(string(setting.SettingKey)) {
		if s.isProductionMode() {
			return fmt.Errorf(
				"refusing to store secret-classified setting %s in plaintext: settings encryption is not configured; "+
					"configure the settings encryption key, restart, then retry (POST /admin/settings/reencrypt encrypts existing values)",
				setting.SettingKey)
		}
		logger.Warn("Storing secret-classified setting %s in plaintext: settings encryption is not configured (allowed outside production builds)", setting.SettingKey)
	}

	// Encrypt value before saving to database
	dbSetting := *setting
	// Set is always an operator-initiated write (the admin API or any other
	// caller of Set) — stamp it explicit regardless of what the caller passed
	// in, so an operator overwriting a seeded row promotes it (#794).
	dbSetting.Origin = models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true}
	if encryptionEnabled {
		encrypted, err := s.encryptor.Encrypt(string(setting.Value))
		if err != nil {
			return fmt.Errorf("failed to encrypt setting %s: %w", setting.SettingKey, err)
		}
		dbSetting.Value = models.DBText(encrypted)
	}

	// Upsert the setting
	// Save lets autoUpdateTime overwrite ModifiedAt with the session clock, so
	// pin that clock to one explicit microsecond instant; the value copied back
	// to the caller then equals the stored row (#1155).
	now := time.Now().UTC().Truncate(time.Microsecond)
	dbSetting.ModifiedAt = now
	result := s.gormDB.WithContext(ctx).
		Session(&gorm.Session{NowFunc: func() time.Time { return now }}).
		Save(&dbSetting)
	if result.Error != nil {
		return fmt.Errorf("failed to save setting %s: %w", setting.SettingKey, result.Error)
	}

	// Update the caller's ModifiedAt and Origin to match what was saved
	setting.ModifiedAt = dbSetting.ModifiedAt
	setting.Origin = dbSetting.Origin

	// Invalidate cache
	s.invalidateCache(ctx, string(setting.SettingKey))
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

// HasRow reports whether a row exists for key. It reads the database directly:
// no bootstrap guard, no decryption and no cache, so it works for bootstrap
// keys and for rows that cannot be decrypted.
func (s *SettingsService) HasRow(ctx context.Context, key string) (bool, error) {
	var n int64
	if err := s.gormDB.WithContext(ctx).Model(&models.SystemSetting{}).Where("setting_key = ?", key).Count(&n).Error; err != nil {
		return false, fmt.Errorf("failed to check setting %s: %w", key, err)
	}
	return n > 0, nil
}

// SeedDefaults seeds the default settings if they don't exist
func (s *SettingsService) SeedDefaults(ctx context.Context) error {
	logger := slogging.Get()
	defaults := models.DefaultSystemSettings()

	// Track keys already provided by the explicit defaults so we can
	// de-duplicate when adding operational-config settings below.
	seenKeys := make(map[string]bool, len(defaults))
	for _, d := range defaults {
		seenKeys[string(d.SettingKey)] = true
	}

	// Append operational-config defaults derived from the classification registry.
	// Bootstrap-category settings are excluded by DefaultOperationalSettings().
	// Keys with empty values are skipped — nothing meaningful to seed.
	// The explicit models.DefaultSystemSettings() entry wins on collision.
	for _, ms := range config.DefaultOperationalSettings() {
		if ms.Value == "" {
			continue
		}
		if seenKeys[ms.Key] {
			logger.Debug("SeedDefaults: skipping config-derived operational key %q — already in explicit defaults", ms.Key)
			continue
		}
		seenKeys[ms.Key] = true
		desc := ms.Description
		defaults = append(defaults, models.SystemSetting{
			SettingKey:  models.DBVarchar(ms.Key),
			Value:       models.DBText(ms.Value),
			SettingType: models.DBVarchar(ms.Type), // same verbatim token as DefaultSystemSettings (#811)
			Description: models.NewNullableDBText(&desc),
			ModifiedAt:  time.Now().UTC().Truncate(time.Microsecond),
		})
	}

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
		// Stamp seeded origin on every row this loop inserts — both the
		// explicit models.DefaultSystemSettings() entries and the
		// config-derived operational defaults appended above feed into
		// `defaults`, so this single assignment covers both sources (#794).
		dbSetting.Origin = models.NullableDBVarchar{String: models.SystemSettingOriginSeeded, Valid: true}
		if s.encryptor != nil && s.encryptor.IsEnabled() {
			encrypted, err := s.encryptor.Encrypt(string(setting.Value))
			if err != nil {
				return fmt.Errorf("failed to encrypt default setting %s: %w", setting.SettingKey, err)
			}
			dbSetting.Value = models.DBText(encrypted)
		}

		// Create the setting
		dbSetting.ModifiedAt = time.Now().UTC().Truncate(time.Microsecond)
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

// ErrEncryptionNotEnabled is returned by ReEncryptAll when no settings
// encryptor is configured. Handlers distinguish it from a database failure
// (#845): the former is a 409 precondition, the latter is 503/500.
var ErrEncryptionNotEnabled = errors.New("encryption is not enabled")

// reEncryptBatchSize bounds one SELECT of stale keys; the pass loops until none remain.
const reEncryptBatchSize = 100

// maxUnreadableSettings caps the NOT IN exclusion list (Oracle allows 1000).
const maxUnreadableSettings = 900

// ErrTooManyUnreadableSettings is returned by ReEncryptAll when it stops at the
// maxUnreadableSettings cap. It is an operator-fixable data condition (409),
// not a database failure.
var ErrTooManyUnreadableSettings = errors.New("too many unreadable settings")

// errSettingUnreadable marks a row that cannot be decrypted or re-encrypted:
// reported, skipped, never a database failure.
var errSettingUnreadable = errors.New("setting unreadable")

// ReEncryptAll re-encrypts every system_settings value not already under the
// current key id. Each row is its own short transaction (#965): a crash or a
// database error leaves a readable mix of old-id and current-id rows and the
// next call finishes the job, because the selection is by envelope prefix, not
// by a full-table pass. Only the value column is written (#805): modified_at and
// modified_by are left as they were. Rows no key can open are reported in
// []SettingError and excluded from later batches so the loop always terminates.
// Returns the number of rows committed so far, also on error.
func (s *SettingsService) ReEncryptAll(ctx context.Context) (int, []SettingError, error) {
	logger := slogging.Get()
	if s.encryptor == nil || !s.encryptor.IsEnabled() {
		return 0, nil, ErrEncryptionNotEnabled
	}
	prefix := s.encryptor.CurrentPrefix()
	var reencrypted int
	var settingErrors []SettingError
	var skip []string
	for {
		var keys []string
		if err := db.WithRetryableGormRead(ctx, db.DefaultRetryConfig(), func() error {
			// Rebuild the chain per attempt: a GORM instance keeps db.Error
			// after a failure, so a reused chain would replay the old error.
			keys = nil
			q := s.gormDB.WithContext(ctx).Model(&models.SystemSetting{}).
				Where("value NOT LIKE ?", prefix+"%").
				Order("setting_key").Limit(reEncryptBatchSize)
			if len(skip) > 0 {
				q = q.Where("setting_key NOT IN ?", skip)
			}
			return q.Pluck("setting_key", &keys).Error
		}); err != nil {
			s.InvalidateAll(ctx)
			return reencrypted, settingErrors, fmt.Errorf("failed to list settings for re-encryption: %w", err)
		}
		if len(keys) == 0 {
			break
		}
		for _, key := range keys {
			err := s.reEncryptOne(ctx, key)
			switch {
			case err == nil:
				reencrypted++
			case errors.Is(err, errSettingUnreadable):
				logger.Warn("Setting %s skipped during re-encryption: %v", key, err)
				settingErrors = append(settingErrors, SettingError{Key: key, Error: err.Error()})
				skip = append(skip, key)
				if len(skip) >= maxUnreadableSettings { // stays under Oracle's 1000-element IN list
					s.InvalidateAll(ctx)
					return reencrypted, settingErrors, fmt.Errorf("%w: re-encryption stopped at the %d unreadable-setting cap; repair or delete the unreadable settings, then retry", ErrTooManyUnreadableSettings, maxUnreadableSettings)
				}
			default:
				logger.Error("Re-encryption stopped after %d rows: %v", reencrypted, err)
				s.InvalidateAll(ctx)
				return reencrypted, settingErrors, err
			}
		}
	}
	s.InvalidateAll(ctx)
	logger.Info("Re-encryption completed: %d re-encrypted, %d errors", reencrypted, len(settingErrors))
	return reencrypted, settingErrors, nil
}

// reEncryptOne rewrites one row's ciphertext under the current key inside its
// own transaction, holding a row lock on PostgreSQL/Oracle so a concurrent
// SettingsService.Set cannot be overwritten.
func (s *SettingsService) reEncryptOne(ctx context.Context, key string) error {
	// READ COMMITTED, not the SERIALIZABLE default: under SERIALIZABLE, Oracle
	// fails SELECT ... FOR UPDATE on a row committed after the tx start SCN
	// with ORA-08177 instead of waiting (the false-08177 class, #903/#906).
	// FOR UPDATE on the primary key already serializes writers of this row.
	return db.WithRetryableGormTransaction(ctx, s.gormDB, db.DefaultRetryConfig(), func(tx *gorm.DB) error {
		q := tx.Where("setting_key = ?", key)
		if tx.Name() != "sqlite" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		// Find, not First: First adds ORDER BY + a row limit, and Oracle
		// cannot combine the row_limiting_clause with FOR UPDATE (ORA-02014).
		// setting_key is the primary key, so at most one row comes back.
		var rows []models.SystemSetting
		if err := q.Find(&rows).Error; err != nil {
			return fmt.Errorf("failed to read setting %s: %w", key, err)
		}
		if len(rows) == 0 {
			return fmt.Errorf("%w: setting no longer exists", errSettingUnreadable)
		}
		row := rows[0]
		plaintext, err := s.encryptor.Decrypt(string(row.Value))
		if err != nil {
			return fmt.Errorf("%w: %w", errSettingUnreadable, err)
		}
		encrypted, err := s.encryptor.Encrypt(plaintext)
		if err != nil {
			return fmt.Errorf("%w: %w", errSettingUnreadable, err)
		}
		// UpdateColumn skips hooks: autoUpdateTime never moves modified_at (#805).
		res := tx.Model(&models.SystemSetting{}).Where("setting_key = ?", key).UpdateColumn("value", models.DBText(encrypted))
		if res.Error != nil {
			return fmt.Errorf("failed to save re-encrypted setting %s: %w", key, res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: setting no longer exists", errSettingUnreadable)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

// CountValuesWithContextID counts rows still encrypted under the given key id;
// the rotator uses it to decide when the previous key can be dropped.
func (s *SettingsService) CountValuesWithContextID(ctx context.Context, id int) (int64, error) {
	var n int64
	err := db.WithRetryableGormRead(ctx, db.DefaultRetryConfig(), func() error {
		return s.gormDB.WithContext(ctx).Model(&models.SystemSetting{}).
			Where("value LIKE ?", fmt.Sprintf("ENC:v1:%d:%%", id)).Count(&n).Error
	})
	if err != nil {
		return 0, fmt.Errorf("failed to count settings under key id %d: %w", id, err)
	}
	return n, nil
}

// PlaintextKeys returns which of the given keys have a non-empty stored value
// without the ENC: prefix, i.e. rows written before settings encryption was
// enabled that ReEncryptAll would convert. It reads the raw rows (List and Get
// decrypt, which hides the prefix) and returns key names only, never values.
func (s *SettingsService) PlaintextKeys(ctx context.Context, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	var rows []models.SystemSetting
	if err := s.gormDB.WithContext(ctx).Where("setting_key IN ?", keys).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to read settings for plaintext check: %w", err)
	}
	var plaintext []string
	for _, row := range rows {
		if v := string(row.Value); v != "" && !crypto.IsEncrypted(v) {
			plaintext = append(plaintext, string(row.SettingKey))
		}
	}
	return plaintext, nil
}

// validateValue validates that the value matches the declared type
func (s *SettingsService) validateValue(setting *models.SystemSetting) error {
	switch setting.SettingType {
	case models.SystemSettingTypeInt:
		if _, err := strconv.Atoi(string(setting.Value)); err != nil {
			return fmt.Errorf("value is not a valid integer")
		}
	case models.SystemSettingTypeBool:
		if _, err := strconv.ParseBool(string(setting.Value)); err != nil {
			return fmt.Errorf("value is not a valid boolean")
		}
	case models.SystemSettingTypeFloat:
		if _, err := strconv.ParseFloat(string(setting.Value), 64); err != nil {
			return fmt.Errorf("value is not a valid number")
		}
	case models.SystemSettingTypeJSON:
		var js json.RawMessage
		if err := json.Unmarshal([]byte(string(setting.Value)), &js); err != nil {
			return fmt.Errorf("value is not valid JSON")
		}
	case models.SystemSettingTypeString:
		// Empty string is not allowed for string settings.
		// On Oracle ADB an empty string bound to a CLOB/VARCHAR2 column is treated as NULL,
		// which violates the NOT NULL constraint on system_settings.value (ORA-01400).
		// Use DELETE to clear a string setting instead of setting it to empty string.
		if setting.Value == "" {
			return fmt.Errorf("empty value not allowed for string setting %q; use DELETE to clear the setting", setting.SettingKey)
		}
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

// setMissingInCache stores a negative-cache tombstone for key: a zero
// SystemSetting (empty SettingKey) that Get reads back as "not found" (#770).
// On the memory tier invalidation is process-local, so a row written by
// another replica or by dbtool becomes visible here only when the tombstone
// expires (up to memCacheTTL), the same window positive entries already have.
func (s *SettingsService) setMissingInCache(ctx context.Context, key string) {
	if s.useMemCache {
		s.memCacheMu.Lock()
		s.memCache[key] = settingsCacheEntry{expiresAt: time.Now().Add(s.memCacheTTL)}
		s.memCacheMu.Unlock()
		return
	}
	// "null" unmarshals into a zero SystemSetting in getFromRedisCache.
	if err := s.redis.Set(ctx, SettingsCacheKey+key, []byte("null"), SettingsCacheTTL); err != nil {
		slogging.Get().Error("Failed to cache missing setting in Redis: %v", err)
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

	s.memCache[string(setting.SettingKey)] = settingsCacheEntry{
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
		if errors.Is(err, redis.Nil) {
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
	cacheKey := SettingsCacheKey + string(setting.SettingKey)

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

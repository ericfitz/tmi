package main

import (
	"context"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
)

// secretKeyGetter is the subset of SettingsServiceInterface needed by the startup check.
// Using a minimal interface makes warnIfPlaintextSecretsAtRest unit-testable without a real DB.
// SEM@99c7bf92a70c0288330ba2861b823dbda8ce3aa2: fetch a single system setting by key (reads DB)
type secretKeyGetter interface {
	Get(ctx context.Context, key string) (*models.SystemSetting, error)
}

// warnIfPlaintextSecretsAtRest checks whether any Secret-classified settings have a
// non-empty plaintext value stored in the database while encryption is disabled.
//
// Severity is scaled by build mode:
//   - dev / test build → WARN
//   - production build → ERROR
//
// The function NEVER returns an error; a warning is informational only and must not
// abort startup. Call this after both the settings service and its encryptor are ready.
// SEM@99c7bf92a70c0288330ba2861b823dbda8ce3aa2: warn when secret-classified settings are stored as plaintext while encryption is disabled (reads DB)
func warnIfPlaintextSecretsAtRest(
	ctx context.Context,
	encryptor *crypto.SettingsEncryptor,
	svc secretKeyGetter,
	cfg *config.Config,
	logger slogging.SimpleLogger,
) {
	// Fast path: encryption is active — nothing to warn about.
	if encryptor != nil && encryptor.IsEnabled() {
		return
	}

	// Collect the names of Secret-classified settings keys that have values in the DB.
	secretKeys := secretClassifiedKeys(cfg)
	if len(secretKeys) == 0 {
		return
	}

	var plaintextKeys []string
	for _, key := range secretKeys {
		setting, err := svc.Get(ctx, key)
		if err != nil {
			// Best-effort: skip keys we cannot read (DB may be unavailable for that key).
			logger.Debug("startup check: could not read setting %s: %v", key, err)
			continue
		}
		if setting != nil && setting.Value != "" {
			plaintextKeys = append(plaintextKeys, key)
		}
	}

	if len(plaintextKeys) == 0 {
		return // No secrets stored → no warning needed.
	}

	msg := "SECURITY WARNING: Secret-classified settings are stored as plaintext in the " +
		"database because settings encryption is not configured. " +
		"Affected keys: [" + strings.Join(plaintextKeys, ", ") + "]. " +
		"To remediate: set the TMI_SECRET_SETTINGS_ENCRYPTION_KEY environment variable " +
		"(or configure a secrets vault), then call POST /admin/settings/reencrypt to " +
		"encrypt existing values at rest."

	isProduction := cfg.Auth.BuildMode == "production"
	if isProduction {
		logger.Error("%s", msg)
	} else {
		logger.Warn("%s", msg)
	}
}

// secretClassifiedKeys returns the list of setting keys that are marked Secret in the
// migratable settings registry derived from the current configuration.
// SEM@99c7bf92a70c0288330ba2861b823dbda8ce3aa2: list config keys classified as secret from the migratable settings registry (pure)
func secretClassifiedKeys(cfg *config.Config) []string {
	all := cfg.GetMigratableSettings()
	keys := make([]string, 0, len(all))
	for _, s := range all {
		if s.Secret {
			keys = append(keys, s.Key)
		}
	}
	return keys
}

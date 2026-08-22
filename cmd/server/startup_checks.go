package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
)

// secretKeyGetter is the subset of SettingsServiceInterface needed by the startup checks
// in this file. Using a minimal interface makes them unit-testable without a real DB.
// SEM@99c7bf92a70c0288330ba2861b823dbda8ce3aa2: fetch a single system setting by key (reads DB)
type secretKeyGetter interface {
	Get(ctx context.Context, key string) (*models.SystemSetting, error)
}

// settingsLister is the subset of SettingsServiceInterface needed by the
// divergence check. It reads every row in one query rather than one query per
// key: GetMigratableSettings emits 131 keys and the explicit subset of a
// container deployment is realistically 30-80, which as serial uncached round
// trips costs seconds of startup on Oracle ADB — materially more on a cold
// Always Free instance that has just auto-started, where a slow boot can trip
// a readiness deadline into a restart loop (oracle-db-admin review, note 4).
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: list every stored system setting in one read (reads DB)
type settingsLister interface {
	List(ctx context.Context) ([]models.SystemSetting, error)
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

// warnIfConfigDatabaseDiverges checks whether any operational setting has both an
// explicit config/environment value and a database row whose value disagrees with it.
//
// This is the check that would have caught the 2026-08-20 outage: an operator had
// TMI_OAUTH_CALLBACK_URL set correctly the whole time, but a seeded default row in
// the database silently outranked it, so OAuth redirected to localhost and every
// provider rejected the login. A second regression in the same deploy left SAML
// silently disabled for the same reason. Nothing warned. This does.
//
// A config value only counts if MigratableSetting.Explicit is true (an operator
// actually set it, not just the struct default), because a default disagreeing with
// the database is normal and not worth flagging. Bootstrap-category settings are
// skipped entirely — they are never database-backed. For each remaining divergence,
// the database row's own Origin tells us which value actually wins: an explicit DB
// row (SystemSetting.IsExplicit()) outranks the config value; a merely-seeded row
// does not, so the config value wins there — see the precedence table on #794.
//
// No setting's value is ever logged, secret or not. Making log safety depend on
// per-key classification metadata was one mistake away from a credential leak,
// and the mistake was already present — see the comment at the divergence
// append below.
//
// The function NEVER returns an error; a warning is informational only and must not
// abort startup. Call this after both the settings service and config are ready —
// ideally after SeedDefaults, so DB rows actually exist to compare against.
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: warn when an explicit config value and an explicit DB row disagree for an operational setting (reads DB)
func warnIfConfigDatabaseDiverges(
	ctx context.Context,
	svc settingsLister,
	cfg *config.Config,
	logger slogging.SimpleLogger,
) {
	settings := cfg.GetMigratableSettings()

	// One query for every row, rather than one per explicitly-configured key.
	// Best-effort: if it fails there is nothing to compare against and the
	// check simply does not run — it must never abort startup.
	rows, err := svc.List(ctx)
	if err != nil {
		logger.Debug("startup check: could not list settings for divergence check: %v", err)
		return
	}
	stored := make(map[string]*models.SystemSetting, len(rows))
	for i := range rows {
		stored[string(rows[i].SettingKey)] = &rows[i]
	}

	var divergences []string
	for _, s := range settings {
		if s.Class.Category != config.CategoryOperational {
			continue // bootstrap keys are config-only; there is no DB row to diverge from
		}
		if !s.Explicit {
			continue // struct default; disagreeing with the DB is normal, not a warning
		}

		row, ok := stored[s.Key]
		if !ok || string(row.Value) == "" {
			continue // no DB row yet, or an empty value — nothing to compare
		}

		dbValue := string(row.Value)
		if dbValue == s.Value {
			continue // config and database agree
		}

		winner := "config (the database row is only a seeded default)"
		if row.IsExplicit() {
			winner = "database (also set explicitly)"
		}

		// Only the key and the winner — never either value, for any key.
		//
		// An earlier revision printed values for keys that were not
		// Secret-classified. That was one metadata mistake away from a
		// credential leak, and the mistake was already present: the
		// auth.oauth.providers. / auth.saml.providers. / content_oauth.providers.
		// subtrees carry Class.Secret=false (a blanket true there would mis-mask
		// non-secret sub-keys like .client_id), so a check keyed on
		// classification printed real OAuth client secrets in plaintext. That is
		// also what CodeQL's go/clear-text-logging objects to: not the specific
		// keys, but making log safety depend on every key being classified
		// correctly forever.
		//
		// The key name and the winner are what an operator actually needs — they
		// say which setting to inspect and which layer is in force. The values
		// are one `SELECT` and one `printenv` away, in a context where seeing
		// them is a deliberate act rather than a side effect of booting.
		divergences = append(divergences, fmt.Sprintf("%s (winner=%s)", s.Key, winner))
	}

	if len(divergences) == 0 {
		return
	}

	msg := fmt.Sprintf(
		"CONFIG WARNING: %d operational setting(s) have an explicit config/environment "+
			"value that disagrees with the value stored in the database. Check whether an "+
			"admin deliberately edited the value at runtime (in which case the database is "+
			"correct and the config/env value is stale), or whether the database row is a "+
			"leftover seeded default from before this config/env value was introduced (in "+
			"which case it should be corrected or deleted). Values are deliberately not "+
			"logged; inspect them via GET /admin/settings/{key} and the process environment. "+
			"Affected: [%s]",
		len(divergences), strings.Join(divergences, "; "),
	)
	logger.Warn("%s", msg)
}

// secretClassifiedKeys returns the list of setting keys that are marked Secret in the
// migratable settings registry derived from the current configuration.
// SEM@2daf3be663df9da54323f16d115f12d78d435c3f: list config keys classified as secret from the migratable settings registry (pure)
func secretClassifiedKeys(cfg *config.Config) []string {
	all := cfg.GetMigratableSettings()
	keys := make([]string, 0, len(all))
	for _, s := range all {
		if s.IsSecret() {
			keys = append(keys, s.Key)
		}
	}
	return keys
}

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/secrets"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"gorm.io/gorm"
)

// snapshotMarkerLine is the first line of a dev settings snapshot written by
// `--export-config --explicit-only`. `--restore-snapshot` refuses any file that
// does not start with it (#1180).
//
// The marker is required, not decorative: the older exports (full files with no
// origin, including .prev and .pre-pr6 leftovers) hold every row, seeded ones
// included. Restoring one of those would stamp every registry default explicit,
// which is the bug this format exists to prevent. It is a comment so the config
// loader ignores it. The v1 suffix lets a later format change be refused the
// same way.
const snapshotMarkerLine = "# tmi-dbtool-snapshot: format=explicit-only/v1"

// hasSnapshotMarker reports whether the file's first line is the marker.
func hasSnapshotMarker(path string) (bool, error) {
	f, err := os.Open(path) // #nosec G304 -- operator-supplied snapshot path
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return false, sc.Err()
	}
	return strings.TrimSpace(sc.Text()) == snapshotMarkerLine, nil
}

// fileHasKey reports whether the snapshot names yamlPath, either as a leaf or
// as a parent of leaves (a json-typed setting holding a map is exported as a
// nested map).
func fileHasKey(fileKeys map[string]bool, yamlPath string) bool {
	if fileKeys[yamlPath] {
		return true
	}
	prefix := yamlPath + "."
	for k := range fileKeys {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// runSnapshotRestore restores a dev settings snapshot (--restore-snapshot).
//
// Semantics, per the settings-precedence ADR decision 8:
//   - writes exactly the keys the file names, as explicit; struct defaults and
//     the host environment never contribute a row or a value;
//   - replaces an existing row whose origin is not explicit (seeded, NULL or
//     unrecognized: the same IsExplicit test the read path uses);
//   - skips an existing explicit row, so a surviving database stays
//     authoritative and a second run is a no-op;
//   - refuses a file without snapshotMarkerLine;
//   - treats an empty snapshot as nothing to do;
//   - applies all writes in one transaction, so a failed restore leaves no
//     half-restored database.
//
// Logs name keys and counts, never values.
func runSnapshotRestore(db *testdb.TestDB, inputFile string, dryRun bool) error {
	log := slogging.Get()

	ok, err := hasSnapshotMarker(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read snapshot %s: %w", inputFile, err)
	}
	if !ok {
		return fmt.Errorf("%s is not a dev settings snapshot (first line is not %q): it predates the "+
			"explicit-only format, and restoring it would stamp seeded defaults explicit. "+
			"Re-take it with `make dev-config-snapshot CLUSTER=<cluster>`", inputFile, snapshotMarkerLine)
	}
	if err := rejectEncryptedSource(inputFile); err != nil {
		return err
	}

	// File only, no environment: the values written are the snapshot's.
	cfg, err := config.LoadSettingsFileOnly(inputFile)
	if err != nil {
		return fmt.Errorf("failed to load snapshot %s: %w", inputFile, err)
	}
	fileKeys, err := config.KeysInFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to read snapshot keys from %s: %w", inputFile, err)
	}

	toWrite := selectSnapshotSettings(cfg, fileKeys)

	if err := validateSettingValues(toWrite, settingsProductionMode(cfg.Auth.BuildMode)); err != nil {
		return err
	}

	if dryRun {
		log.Info("[DRY RUN] Would restore up to %d setting(s) from %s (existing explicit rows are skipped):", len(toWrite), inputFile)
		for _, s := range toWrite {
			log.Info("  %s [%s]", s.Key, s.Type)
		}
		return nil
	}
	if len(toWrite) == 0 {
		log.Info("Snapshot %s holds no settings; nothing to restore", inputFile)
		return nil
	}

	// The secrets-provider settings come from the process environment, not the
	// snapshot: the file-only cfg above has none.
	envCfg, err := config.LoadSettingsSource("")
	if err != nil {
		return fmt.Errorf("failed to load environment config: %w", err)
	}
	encryptor := newSettingsEncryptor(envCfg)

	var created, replaced, skipped int
	txErr := db.DB().Transaction(func(tx *gorm.DB) error {
		var err error
		created, replaced, skipped, err = applySnapshotSettings(tx, toWrite, encryptor)
		return err
	})
	if txErr != nil {
		return txErr
	}
	log.Info("Snapshot restored: %d created, %d replaced non-explicit rows, %d existing explicit rows skipped", created, replaced, skipped)
	return nil
}

// newSettingsEncryptor returns the settings encryptor for cfg's secrets
// provider, or nil when none is available (settings are then written in
// plaintext, as --import-config does).
func newSettingsEncryptor(cfg *config.Config) *crypto.SettingsEncryptor {
	log := slogging.Get()
	provider, err := secrets.NewProvider(context.Background(), &cfg.Secrets)
	if err != nil {
		log.Debug("No secrets provider available for encryption: %v", err)
		return nil
	}
	defer func() {
		if closeErr := provider.Close(); closeErr != nil {
			log.Debug("Failed to close secrets provider: %v", closeErr)
		}
	}()
	enc, encErr := crypto.NewSettingsEncryptor(context.Background(), provider)
	if encErr != nil {
		log.Debug("No encryptor available: %v", encErr)
		return nil
	}
	log.Info("Settings encryption enabled")
	return enc
}

// selectSnapshotSettings returns the settings the snapshot names that restore
// can write: operational, non-empty. Keys the file names that map to no
// restorable setting are logged (names only) and ignored.
func selectSnapshotSettings(cfg *config.Config, fileKeys map[string]bool) []config.MigratableSetting {
	var toWrite []config.MigratableSetting
	matched := make(map[string]bool) // file keys accounted for
	for _, s := range cfg.GetMigratableSettings() {
		yp := config.YAMLPathForKey(s.Key)
		if !fileHasKey(fileKeys, yp) {
			continue
		}
		for k := range fileKeys {
			if k == yp || strings.HasPrefix(k, yp+".") {
				matched[k] = true
			}
		}
		if s.Class.Category == config.CategoryBootstrap || s.Value == "" {
			continue
		}
		toWrite = append(toWrite, s)
	}
	var unmatched []string
	for k := range fileKeys {
		if !matched[k] {
			unmatched = append(unmatched, k)
		}
	}
	if len(unmatched) > 0 {
		sort.Strings(unmatched)
		slogging.Get().Warn("Snapshot names %d key(s) with no restorable setting, ignored: %s",
			len(unmatched), strings.Join(unmatched, ", "))
	}
	return toWrite
}

// applySnapshotSettings writes the settings inside tx: it skips an existing
// explicit row, replaces any other existing row, and creates missing rows, all
// stamped explicit. It returns the created, replaced and skipped counts.
func applySnapshotSettings(tx *gorm.DB, toWrite []config.MigratableSetting, encryptor *crypto.SettingsEncryptor) (created, replaced, skipped int, err error) {
	log := slogging.Get()
	for _, s := range toWrite {
		var existing models.SystemSetting
		res := tx.Where("setting_key = ?", s.Key).Limit(1).Find(&existing)
		if res.Error != nil {
			return 0, 0, 0, fmt.Errorf("failed to read setting %s: %w", s.Key, res.Error)
		}
		exists := res.RowsAffected > 0
		if exists && existing.IsExplicit() {
			skipped++
			log.Debug("  Skipping existing explicit: %s", s.Key)
			continue
		}

		value := s.Value
		if s.Secret && encryptor == nil {
			log.Warn("Writing secret setting %s in plaintext (no settings encryptor available)", s.Key)
		}
		if s.Secret && encryptor != nil {
			enc, encErr := encryptor.Encrypt(value)
			if encErr != nil {
				return 0, 0, 0, fmt.Errorf("failed to encrypt setting %s: %w", s.Key, encErr)
			}
			value = enc
		}
		description := s.Description

		if exists {
			// Conditional replace: only a row that is still not explicit. The
			// explicit NULL branch keeps this identical on Oracle and PostgreSQL
			// (<> alone is unknown for NULL). 0 rows affected means the row
			// became explicit since the Find, so it is skipped.
			upd := tx.Model(&models.SystemSetting{}).
				Where("setting_key = ? AND (origin IS NULL OR origin <> ?)", s.Key, models.SystemSettingOriginExplicit).
				Updates(map[string]any{
					"value":        value,
					"setting_type": s.Type,
					"description":  description,
					"origin":       models.SystemSettingOriginExplicit,
				})
			if upd.Error != nil {
				return 0, 0, 0, fmt.Errorf("failed to update setting %s: %w", s.Key, upd.Error)
			}
			if upd.RowsAffected == 0 {
				skipped++
				continue
			}
			replaced++
			continue
		}
		row := models.SystemSetting{
			SettingKey:  models.DBVarchar(s.Key),
			Value:       models.DBText(value),
			SettingType: models.DBVarchar(s.Type),
			Description: models.NewNullableDBText(&description),
			ModifiedAt:  time.Now(),
			Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
		}
		if createErr := tx.Create(&row).Error; createErr != nil {
			return 0, 0, 0, fmt.Errorf("failed to create setting %s: %w", s.Key, createErr)
		}
		created++
	}
	return created, replaced, skipped, nil
}

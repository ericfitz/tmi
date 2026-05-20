package main

import (
	"fmt"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// legacyImportOptions captures every flag that affects --import-legacy.
type legacyImportOptions struct {
	inputFile  string
	outputFile string
	overwrite  bool
	dryRun     bool
	noBackup   bool
	noRewrite  bool
}

// runLegacyConfigImport imports the operational-category settings from a
// legacy config-*.yml into the database settings table and, by default,
// rewrites the source file in place with the operational keys removed
// (after writing a timestamped backup). It is the one-stop migration path
// for the #415 bootstrap-only config collapse.
//
// Flow with no extra flags:
//  1. Write a timestamped backup of the source: <path>.<YYYYMMDD-HHMMSS>.bak.
//  2. Import operational settings to the DB.
//  3. Rewrite the source file in place with the operational keys removed,
//     preserving bootstrap key types/values verbatim.
//
// Flag overrides:
//   - --no-backup       skip the backup.
//   - --no-rewrite      keep legacy behavior — write a sibling *-migrated.yml
//     and leave the source untouched.
//   - --output <path>   write the migrated YAML to <path> and leave the
//     source untouched (mutually exclusive with --no-rewrite,
//     enforced by the caller).
//   - --dry-run         do nothing destructive; print what would happen.
func runLegacyConfigImport(db *testdb.TestDB, opts legacyImportOptions) error {
	log := slogging.Get()

	opKeys, err := config.OperationalKeysInFile(opts.inputFile)
	if err != nil {
		return fmt.Errorf("scan legacy config %s: %w", opts.inputFile, err)
	}
	log.Info("Legacy config %s contains %d operational keys to import", opts.inputFile, len(opKeys))

	// Determine which output mode is in play.
	inPlace := opts.outputFile == "" && !opts.noRewrite && looksLikeYAMLPath(opts.inputFile)

	// Backup the source FIRST whenever the source will be modified — that's
	// the default in-place case. The backup is skipped when --no-backup is
	// set or when the source will not be modified (--no-rewrite, --output,
	// non-YAML input).
	if inPlace && !opts.noBackup && !opts.dryRun {
		bak, bakErr := backupConfigFile(opts.inputFile)
		if bakErr != nil {
			return fmt.Errorf("backup source: %w", bakErr)
		}
		log.Info("Backup written to %s", bak)
	} else if inPlace && opts.noBackup && !opts.dryRun {
		log.Warn("--no-backup set: not backing up %s before in-place rewrite", opts.inputFile)
	}

	// Step 2: DB import. runConfigSeed handles the bootstrap/operational
	// split, encryption, empty-skip (#424), and dry-run.
	//
	// When inPlace is true we pass an empty outputFile and do NOT write a
	// sibling *-migrated.yml — the source rewrite below covers it.
	// When --no-rewrite or --output is in effect, runConfigSeed writes the
	// migrated YAML at the legacy location.
	configSeedOutput := opts.outputFile
	if inPlace {
		configSeedOutput = "" // suppress the legacy *-migrated.yml; rewrite source instead.
	}
	if err := runConfigSeed(db, opts.inputFile, configSeedOutput, opts.overwrite, opts.dryRun, !inPlace); err != nil {
		return err
	}

	// Step 3: in-place source rewrite (default path).
	if inPlace {
		if opts.dryRun {
			log.Info("[DRY RUN] Would rewrite %s with operational keys removed (no backup written)", opts.inputFile)
			return nil
		}
		size, sErr := stripOperationalKeys(opts.inputFile)
		if sErr != nil {
			return fmt.Errorf("strip operational keys from %s: %w", opts.inputFile, sErr)
		}
		log.Info("Rewrote %s in place (%d bytes, %d operational keys removed)", opts.inputFile, size, len(opKeys))
	}

	return nil
}

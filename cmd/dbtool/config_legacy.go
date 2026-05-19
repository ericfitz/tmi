package main

import (
	"fmt"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runLegacyConfigImport imports the operational-category settings from a
// legacy config-*.yml into the database settings table. Bootstrap keys are
// left in the file. It is the one-time migration path for the #415
// bootstrap-only config collapse.
func runLegacyConfigImport(db *testdb.TestDB, inputFile string, overwrite, dryRun bool) error {
	log := slogging.Get()

	opKeys, err := config.OperationalKeysInFile(inputFile)
	if err != nil {
		return fmt.Errorf("scan legacy config %s: %w", inputFile, err)
	}
	log.Info("Legacy config %s contains %d operational keys to import", inputFile, len(opKeys))

	// runConfigSeed already splits bootstrap vs operational by Class.Category
	// and writes operational settings to the DB. Reuse it.
	// Note: runConfigSeed calls config.Load, which emits its own load-time
	// drift Warn for the same operational keys — so the operator sees both the
	// Info above and that Warn. This double-logging is expected and harmless.
	return runConfigSeed(db, inputFile, "", overwrite, dryRun)
}

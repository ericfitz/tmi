package main

import (
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// runSystemSeed runs the system seed mode, which seeds built-in groups and
// webhook deny list entries via api/seed.SeedDatabase().
func runSystemSeed(db *testdb.TestDB, dryRun bool) error {
	log := slogging.Get()

	if dryRun {
		log.Info("[DRY RUN] Would seed built-in groups and webhook deny list")
		return nil
	}

	log.Info("Seeding built-in groups and webhook deny list...")
	if err := seed.SeedDatabase(db.DB()); err != nil {
		return err
	}

	log.Info("System seed complete")
	return nil
}

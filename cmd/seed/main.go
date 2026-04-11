package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		mode       = flag.String("mode", "", "Seed mode: system, data, config (required)")
		configFile = flag.String("config", "", "Path to TMI configuration file (provides DB connection)")
		inputFile  = flag.String("input", "", "Path to seed data file (data mode) or config YAML (config mode)")
		outputFile = flag.String("output", "", "Path for migrated config YAML (config mode only)")
		serverURL  = flag.String("server", "http://localhost:8080", "TMI server URL for API calls (data mode)")
		user       = flag.String("user", "charlie", "OAuth user ID for API authentication (data mode)")
		provider   = flag.String("provider", "tmi", "OAuth provider name (data mode)")
		overwrite  = flag.Bool("overwrite", false, "Overwrite existing DB settings (config mode)")
		dryRun     = flag.Bool("dry-run", false, "Show what would happen without writing")
		verbose    = flag.Bool("verbose", false, "Enable debug logging")
	)
	flag.Parse()

	if *mode == "" {
		fmt.Fprintln(os.Stderr, "Error: --mode flag is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: tmi-seed --mode=<mode> [OPTIONS]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Modes:")
		fmt.Fprintln(os.Stderr, "  system  Seed built-in groups and webhook deny list")
		fmt.Fprintln(os.Stderr, "  data    Seed entities from a JSON/YAML seed file")
		fmt.Fprintln(os.Stderr, "  config  Migrate config file settings to database")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
		return 1
	}

	// Initialize logging
	logLevel := slogging.LogLevelInfo
	if *verbose {
		logLevel = slogging.LogLevelDebug
	}
	if err := slogging.Initialize(slogging.Config{
		Level:            logLevel,
		IsDev:            true,
		AlsoLogToConsole: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logger: %v\n", err)
	}
	log := slogging.Get()

	// Determine config file for DB connection
	dbConfigFile := *configFile
	if dbConfigFile == "" && *mode == "config" {
		dbConfigFile = *inputFile
	}
	if dbConfigFile == "" {
		log.Error("--config flag is required (or --input for config mode)")
		return 1
	}

	// Connect to database
	log.Info("Connecting to database...")
	db, err := testdb.New(dbConfigFile)
	if err != nil {
		log.Error("Failed to connect to database: %v", err)
		return 1
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Error closing database: %v", closeErr)
		}
	}()
	log.Info("Connected to %s database", db.DialectName())

	// Ensure schema is up to date
	log.Info("Ensuring database schema is up to date...")
	if err := db.AutoMigrate(); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "ORA-00955") || strings.Contains(errStr, "ORA-01442") {
			log.Debug("Oracle migration notice (benign): %v", err)
		} else {
			log.Error("Failed to auto-migrate schema: %v", err)
			return 1
		}
	}

	switch *mode {
	case "system":
		if err := runSystemSeed(db, *dryRun); err != nil {
			log.Error("System seed failed: %v", err)
			return 1
		}

	case "data":
		if *inputFile == "" {
			log.Error("--input flag is required for data mode")
			return 1
		}
		if err := runDataSeed(db, *inputFile, *serverURL, *user, *provider, *dryRun); err != nil {
			log.Error("Data seed failed: %v", err)
			return 1
		}

	case "config":
		if *inputFile == "" {
			log.Error("--input flag is required for config mode")
			return 1
		}
		if err := runConfigSeed(db, *inputFile, *outputFile, *overwrite, *dryRun); err != nil {
			log.Error("Config seed failed: %v", err)
			return 1
		}

	default:
		log.Error("Unknown mode: %s (expected: system, data, config)", *mode)
		return 1
	}

	return 0
}

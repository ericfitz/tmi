package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// Build-time variables injected via -ldflags
var (
	toolVersion = "development"
	toolCommit  = "unknown"
	toolBuiltAt = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Define flags
	schema := flag.Bool("schema", false, "Create/migrate database schema and seed system data")
	flag.BoolVar(schema, "s", false, "Create/migrate database schema and seed system data (short)")

	importConfig := flag.Bool("import-config", false, "Import config file settings into database")
	flag.BoolVar(importConfig, "c", false, "Import config file settings into database (short)")

	importTestData := flag.Bool("import-test-data", false, "Import test data from a seed file")
	flag.BoolVar(importTestData, "t", false, "Import test data from a seed file (short)")

	inputFile := flag.String("input-file", "", "Input file (config YAML for -c, seed JSON for -t)")
	flag.StringVar(inputFile, "f", "", "Input file (short)")

	configFile := flag.String("config", "", "TMI config file (provides DB connection via database.url)")
	outputFile := flag.String("output", "", "Path for migrated config YAML (with -c)")
	overwrite := flag.Bool("overwrite", false, "Overwrite existing settings (with -c)")

	serverURL := flag.String("server", "http://localhost:8080", "TMI server URL for API calls (with -t)")
	user := flag.String("user", "charlie", "OAuth user ID for API authentication (with -t)")
	provider := flag.String("provider", "tmi", "OAuth provider name (with -t)")

	dryRun := flag.Bool("dry-run", false, "Show what would happen without writing")
	verbose := flag.Bool("verbose", false, "Print step-by-step operations and DB messages")
	flag.BoolVar(verbose, "v", false, "Print step-by-step operations (short)")

	flag.Usage = printUsage
	flag.Parse()

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

	// Print startup banner
	schemaModels := len(api.GetAllModels())
	info := ToolInfo{
		Version:      toolVersion,
		Commit:       toolCommit,
		BuiltAt:      toolBuiltAt,
		SchemaModels: schemaModels,
	}
	fmt.Fprintf(os.Stderr, "tmi-dbtool %s (commit: %s, built: %s)\n", info.Version, info.Commit, info.BuiltAt)
	fmt.Fprintf(os.Stderr, "Schema version: %d models\n\n", info.SchemaModels)

	// Build arguments map for exit summary
	args := map[string]any{
		"schema":           *schema,
		"import_config":    *importConfig,
		"import_test_data": *importTestData,
		"dry_run":          *dryRun,
	}
	if *configFile != "" {
		args["config"] = *configFile
	}
	if *inputFile != "" {
		args["input_file"] = *inputFile
	}

	// Determine which operation to run
	opCount := boolCount(*schema, *importConfig, *importTestData)

	// Resolve config file
	dbConfigFile := *configFile
	if dbConfigFile == "" && *importConfig && *inputFile != "" {
		dbConfigFile = *inputFile
	}
	if dbConfigFile == "" {
		printExitSummary(info, args, "failure", "No database connection. Provide --config or set TMI_DATABASE_URL")
		return 1
	}

	// Connect to database
	log := slogging.Get()
	log.Info("Connecting to database...")
	db, err := testdb.New(dbConfigFile)
	if err != nil {
		printExitSummary(info, args, "failure", fmt.Sprintf("Failed to connect to database: %v", err))
		return 1
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Error closing database: %v", closeErr)
		}
	}()
	log.Info("Connected to %s database", db.DialectName())

	// Dispatch
	var runErr error

	switch {
	case opCount == 0:
		// No operation flags - health check mode
		runErr = runHealthCheck(db, *verbose)
	case opCount > 1:
		runErr = fmt.Errorf("only one operation flag can be specified at a time (-s, -c, -t)")
	case *schema:
		runErr = runSchema(db, *dryRun, *verbose)
	case *importConfig:
		if *inputFile == "" {
			runErr = fmt.Errorf("--input-file / -f is required for --import-config")
		} else {
			if !*dryRun {
				if migrateErr := ensureSchema(db); migrateErr != nil {
					log.Warn("Schema migration skipped or failed: %v", migrateErr)
				}
			}
			runErr = runConfigSeed(db, *inputFile, *outputFile, *overwrite, *dryRun)
		}
	case *importTestData:
		if *inputFile == "" {
			runErr = fmt.Errorf("--input-file / -f is required for --import-test-data")
		} else {
			if !*dryRun {
				if migrateErr := ensureSchema(db); migrateErr != nil {
					log.Warn("Schema migration skipped or failed: %v", migrateErr)
				}
			}
			runErr = runDataSeed(db, *inputFile, *serverURL, *user, *provider, *dryRun)
		}
	}

	if runErr != nil {
		log.Error("%v", runErr)
		printExitSummary(info, args, "failure", runErr.Error())
		return 1
	}

	printExitSummary(info, args, "success", "")
	return 0
}

// ensureSchema runs AutoMigrate if needed. Non-fatal for import operations.
func ensureSchema(db *testdb.TestDB) error {
	log := slogging.Get()
	log.Info("Ensuring schema is up to date...")
	if err := db.AutoMigrate(); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "ORA-00955") || strings.Contains(errStr, "ORA-01442") {
			log.Debug("Oracle migration notice (benign): %v", err)
			return nil
		}
		return err
	}
	return nil
}

// printExitSummary prints the JSON exit summary to stdout.
func printExitSummary(info ToolInfo, args map[string]any, status, errMsg string) {
	summary := ExitSummary{
		Tool:         "tmi-dbtool",
		Version:      info.Version,
		Commit:       info.Commit,
		BuiltAt:      info.BuiltAt,
		SchemaModels: info.SchemaModels,
		Arguments:    args,
		Status:       status,
		Error:        errMsg,
	}
	data, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(data))
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `tmi-dbtool - TMI Database Administration Tool

Usage: tmi-dbtool [OPTIONS]

Database Operations:
  -s, --schema              Create/migrate schema and seed system data
  -c, --import-config       Import config file settings into database
  -t, --import-test-data    Import test data from a seed file

Input:
  -f, --input-file FILE     Input file (config YAML for -c, seed JSON for -t)
      --config FILE         TMI config file (provides DB connection via database.url)

Output:
      --output FILE         Path for migrated config YAML (with -c)

Behavior:
      --dry-run             Show what would happen without writing
      --overwrite           Overwrite existing settings (with -c)
  -v, --verbose             Print step-by-step operations and DB messages
  -h, --help                Print usage

Test Data Options (with -t):
      --server URL          TMI server URL for API calls (default: http://localhost:8080)
      --user USER           OAuth user ID (default: charlie)
      --provider PROVIDER   OAuth provider name (default: tmi)

No-argument mode (health check):
  tmi-dbtool --config=config.yml
  Connects to database, prints engine info and schema health report.

Examples:
  tmi-dbtool --config=config-development.yml                    # Health check
  tmi-dbtool -s --config=config-development.yml                 # Schema + seed
  tmi-dbtool -s --config=config-development.yml --dry-run       # Preview changes
  tmi-dbtool -c -f config-production.yml                        # Import config
  tmi-dbtool -t -f test/seeds/cats-seed-data.json --config=config-development.yml
`)
}

func boolCount(flags ...bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}

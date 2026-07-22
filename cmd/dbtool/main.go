package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

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

// SEM@e93cc27eac1d842461899300fefcaebc977cb3db: entry point: delegate to run and exit with its return code
func main() {
	os.Exit(run())
}

// SEM@e7880ae29f527fb2d814f6d7b7c13280082fa033: parse flags, connect to the database, and dispatch a schema/seed/import operation (reads DB)
func run() int {
	// Define flags
	schema := flag.Bool("schema", false, "Create/migrate database schema and seed system data")
	flag.BoolVar(schema, "s", false, "Create/migrate database schema and seed system data (short)")

	importConfig := flag.Bool("import-config", false, "Import config file settings into database")
	flag.BoolVar(importConfig, "c", false, "Import config file settings into database (short)")

	importTestData := flag.Bool("import-test-data", false, "Import test data from a seed file")
	flag.BoolVar(importTestData, "t", false, "Import test data from a seed file (short)")

	importLegacy := flag.Bool("import-legacy", false, "Import operational settings from a legacy config file into the database")
	flag.BoolVar(importLegacy, "l", false, "Import operational settings from a legacy config file (short)")

	exportConfig := flag.Bool("export-config", false, "Export database settings to a config YAML file")
	noDecrypt := flag.Bool("no-decrypt", false, "With --export-config: skip secret settings instead of decrypting")

	inputFile := flag.String("input-file", "", "Input file (config YAML for -c and -l, seed JSON for -t)")
	flag.StringVar(inputFile, "f", "", "Input file (short)")

	configFile := flag.String("config", "", "TMI config file (provides DB connection via database.url)")
	outputFile := flag.String("output", "", "Path for migrated config YAML (with -c or -l); export destination (with --export-config)")
	overwrite := flag.Bool("overwrite", false, "Overwrite existing settings (with -c)")
	noBackup := flag.Bool("no-backup", false, "Skip the timestamped backup of the source file (with -l default flow)")
	noRewrite := flag.Bool("no-rewrite", false, "Keep legacy behavior — write a sibling *-migrated.yml and leave the source untouched (with -l)")

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
		"import_legacy":    *importLegacy,
		"export_config":    *exportConfig,
		"dry_run":          *dryRun,
	}
	if *configFile != "" {
		args["config"] = *configFile
	}
	if *inputFile != "" {
		args["input_file"] = *inputFile
	}

	// Determine which operation to run
	opCount := boolCount(*schema, *importConfig, *importTestData, *importLegacy, *exportConfig)

	// Resolve config file
	dbConfigFile := *configFile
	if dbConfigFile == "" && (*importConfig || *importLegacy || *exportConfig) && *inputFile != "" {
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
	runErr := dispatchOperation(db, log, opCount, cliFlags{
		schema:         *schema,
		importConfig:   *importConfig,
		importTestData: *importTestData,
		importLegacy:   *importLegacy,
		exportConfig:   *exportConfig,
		inputFile:      *inputFile,
		outputFile:     *outputFile,
		overwrite:      *overwrite,
		noBackup:       *noBackup,
		noRewrite:      *noRewrite,
		noDecrypt:      *noDecrypt,
		serverURL:      *serverURL,
		user:           *user,
		provider:       *provider,
		dryRun:         *dryRun,
		verbose:        *verbose,
	})

	if runErr != nil {
		log.Error("%v", runErr)
		printExitSummary(info, args, "failure", runErr.Error())
		return 1
	}

	printExitSummary(info, args, "success", "")
	return 0
}

// cliFlags holds the dereferenced CLI flag values needed to dispatch an operation.
// SEM@7e3bc19f8950c8b27a14cef539ae7dff89e30a7a: DTO holding dereferenced CLI flag values for dispatching a dbtool operation (pure)
type cliFlags struct {
	schema         bool
	importConfig   bool
	importTestData bool
	importLegacy   bool
	exportConfig   bool
	inputFile      string
	outputFile     string
	overwrite      bool
	noBackup       bool
	noRewrite      bool
	noDecrypt      bool
	serverURL      string
	user           string
	provider       string
	dryRun         bool
	verbose        bool
}

// dispatchOperation selects and runs the single requested dbtool operation
// (schema, import-config, import-test-data, import-legacy, or
// export-config), or a health check when no operation flag is set.
// SEM@7e3bc19f8950c8b27a14cef539ae7dff89e30a7a: select and run the requested dbtool operation, or health check if none given (reads DB)
func dispatchOperation(db *testdb.TestDB, log *slogging.Logger, opCount int, f cliFlags) error {
	switch {
	case opCount == 0:
		// No operation flags - health check mode
		return runHealthCheck(db, f.verbose)
	case opCount > 1:
		return fmt.Errorf("only one operation flag can be specified at a time (-s, -c, -t, -l, --export-config)")
	case f.schema:
		return runSchema(db, f.dryRun, f.verbose)
	case f.importConfig:
		return dispatchImportConfig(db, log, f)
	case f.importTestData:
		return dispatchImportTestData(db, log, f)
	case f.importLegacy:
		return dispatchImportLegacy(db, log, f)
	case f.exportConfig:
		return dispatchExportConfig(db, f)
	}
	return nil
}

// dispatchImportConfig runs --import-config after validating --input-file.
// SEM@7e3bc19f8950c8b27a14cef539ae7dff89e30a7a: validate --input-file and run config import into the database (reads/writes DB)
func dispatchImportConfig(db *testdb.TestDB, log *slogging.Logger, f cliFlags) error {
	if f.inputFile == "" {
		return fmt.Errorf("--input-file / -f is required for --import-config")
	}
	if !f.dryRun {
		if migrateErr := ensureSchema(db); migrateErr != nil {
			log.Warn("Schema migration skipped or failed: %v", migrateErr)
		}
	}
	return runConfigSeed(db, f.inputFile, f.outputFile, f.overwrite, f.dryRun, true)
}

// dispatchImportTestData runs --import-test-data after validating --input-file.
// SEM@7e3bc19f8950c8b27a14cef539ae7dff89e30a7a: validate --input-file and run test data import via the API (calls API)
func dispatchImportTestData(db *testdb.TestDB, log *slogging.Logger, f cliFlags) error {
	if f.inputFile == "" {
		return fmt.Errorf("--input-file / -f is required for --import-test-data")
	}
	if !f.dryRun {
		if migrateErr := ensureSchema(db); migrateErr != nil {
			log.Warn("Schema migration skipped or failed: %v", migrateErr)
		}
	}
	return runDataSeed(db, f.inputFile, f.serverURL, f.user, f.provider, f.dryRun)
}

// dispatchImportLegacy runs --import-legacy after validating its flag combination.
// SEM@7e3bc19f8950c8b27a14cef539ae7dff89e30a7a: validate legacy-import flag combination and run legacy config migration (reads/writes DB)
func dispatchImportLegacy(db *testdb.TestDB, log *slogging.Logger, f cliFlags) error {
	switch {
	case f.inputFile == "":
		return fmt.Errorf("--input-file / -f is required for --import-legacy")
	case f.noRewrite && f.outputFile != "":
		return fmt.Errorf("--no-rewrite and --output are mutually exclusive")
	}
	if !f.dryRun {
		if migrateErr := ensureSchema(db); migrateErr != nil {
			log.Warn("Schema migration skipped or failed: %v", migrateErr)
		}
	}
	return runLegacyConfigImport(db, legacyImportOptions{
		inputFile:  f.inputFile,
		outputFile: f.outputFile,
		overwrite:  f.overwrite,
		dryRun:     f.dryRun,
		noBackup:   f.noBackup,
		noRewrite:  f.noRewrite,
	})
}

// dispatchExportConfig runs --export-config after validating --input-file and --output.
// SEM@7e3bc19f8950c8b27a14cef539ae7dff89e30a7a: validate --input-file and --output and run database config export (reads DB, writes file)
func dispatchExportConfig(db *testdb.TestDB, f cliFlags) error {
	switch {
	case f.outputFile == "":
		return fmt.Errorf("--output is required for --export-config")
	case f.inputFile == "":
		return fmt.Errorf("--input-file / -f is required for --export-config")
	}
	return runConfigExport(db, f.inputFile, f.outputFile, !f.noDecrypt)
}

// ensureSchema runs AutoMigrate if needed. Non-fatal for import operations.
// SEM@6415706e07613a139449e1bff6eef269e3783417: run AutoMigrate to bring the database schema up to date (mutates shared state)
func ensureSchema(db *testdb.TestDB) error {
	log := slogging.Get()
	log.Info("Ensuring schema is up to date...")
	return db.AutoMigrate()
}

// printExitSummary prints the JSON exit summary to stdout.
// SEM@e93cc27eac1d842461899300fefcaebc977cb3db: serialize and print a JSON exit summary with tool metadata and status
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

// SEM@e7880ae29f527fb2d814f6d7b7c13280082fa033: print CLI usage text for the dbtool command (pure)
func printUsage() {
	fmt.Fprintf(os.Stderr, `tmi-dbtool - TMI Database Administration Tool

Usage: tmi-dbtool [OPTIONS]

Database Operations:
  -s, --schema              Create/migrate schema and seed system data
  -c, --import-config       Import config file settings into database
  -t, --import-test-data    Import test data from a seed file
  -l, --import-legacy       Import operational settings from a legacy config file into the database
      --export-config       Export database settings to a config YAML file

Input:
  -f, --input-file FILE     Input file (config YAML for -c, -l, and --export-config; seed JSON for -t)
      --config FILE         TMI config file (provides DB connection via database.url)

Output:
      --output FILE         Path for migrated config YAML (with -c or -l);
                            export destination (with --export-config)

Behavior:
      --dry-run             Show what would happen without writing
      --overwrite           Overwrite existing settings (with -c)
      --no-backup           Skip the timestamped source backup (with -l default flow)
      --no-rewrite          Write a sibling *-migrated.yml and leave the source
                            untouched (with -l; mutually exclusive with --output)
      --no-decrypt          With --export-config: skip secret settings instead of decrypting
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
  tmi-dbtool -l -f config-production.yml                        # Import operational settings from a legacy config into the DB
  tmi-dbtool -t -f test/seeds/cats-seed-data.json --config=config-development.yml
  tmi-dbtool --export-config -f config-development.yml --output export.yml   # Export DB settings to YAML
`)
}

// SEM@e93cc27eac1d842461899300fefcaebc977cb3db: count the number of true values in a variadic bool list (pure)
func boolCount(flags ...bool) int {
	n := 0
	for _, f := range flags {
		if f {
			n++
		}
	}
	return n
}

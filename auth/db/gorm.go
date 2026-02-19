package db

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// DatabaseType represents the type of database
type DatabaseType string

const (
	DatabaseTypePostgres  DatabaseType = "postgres"
	DatabaseTypeOracle    DatabaseType = "oracle"
	DatabaseTypeMySQL     DatabaseType = "mysql"
	DatabaseTypeSQLServer DatabaseType = "sqlserver"
	DatabaseTypeSQLite    DatabaseType = "sqlite"
)

// GormConfig holds the configuration for GORM database connection.
// The primary configuration method is via DATABASE_URL which contains all connection
// parameters. The URL is parsed and values are stored in the unified fields below.
type GormConfig struct {
	Type DatabaseType // Database type (extracted from URL scheme)

	// Unified connection parameters (extracted from DATABASE_URL)
	Host     string // Database host
	Port     string // Database port
	User     string // Database username
	Password string //nolint:gosec // G117 - database connection password
	Database string // Database name
	SSLMode  string // SSL mode (PostgreSQL: disable/require/prefer)

	// SQLite-specific
	SQLitePath string // File path or ":memory:" for in-memory database

	// Oracle-specific (cannot be encoded in URL)
	OracleConnectString  string // TNS-style connect string (host:port/service)
	OracleWalletLocation string // Path to Oracle wallet for mTLS (env: TMI_ORACLE_WALLET_LOCATION)

	// Connection pool configuration
	MaxOpenConns    int // Maximum number of open connections to the database (default: 10)
	MaxIdleConns    int // Maximum number of idle connections in the pool (default: 2)
	ConnMaxLifetime int // Maximum time in seconds a connection can be reused (default: 240 = 4 minutes)
	ConnMaxIdleTime int // Maximum time in seconds a connection can be idle (default: 30)
}

// GormDB represents a GORM database connection that works with PostgreSQL, Oracle, MySQL, SQL Server, and SQLite
type GormDB struct {
	db        *gorm.DB
	cfg       GormConfig
	dialector gorm.Dialector
}

// ParseDatabaseURL parses a database connection URL and returns a GormConfig.
// Supported URL formats:
//   - postgres://user:password@host:port/database?sslmode=require
//   - mysql://user:password@host:port/database
//   - sqlserver://user:password@host:port?database=dbname
//   - sqlite:///path/to/file.db or sqlite://:memory:
//   - oracle://user:password@host:port/service_name
func ParseDatabaseURL(rawURL string) (*GormConfig, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("database URL is empty")
	}

	log := slogging.Get()
	log.Debug("Parsing database URL")

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	cfg := &GormConfig{}

	// Determine database type from scheme
	switch strings.ToLower(parsedURL.Scheme) {
	case "postgres", "postgresql":
		cfg.Type = DatabaseTypePostgres
		if err := parsePostgresURL(cfg, parsedURL); err != nil {
			return nil, err
		}
	case "mysql":
		cfg.Type = DatabaseTypeMySQL
		if err := parseMySQLURL(cfg, parsedURL); err != nil {
			return nil, err
		}
	case "sqlserver", "mssql":
		cfg.Type = DatabaseTypeSQLServer
		if err := parseSQLServerURL(cfg, parsedURL); err != nil {
			return nil, err
		}
	case "sqlite", "sqlite3":
		cfg.Type = DatabaseTypeSQLite
		if err := parseSQLiteURL(cfg, parsedURL); err != nil {
			return nil, err
		}
	case "oracle":
		cfg.Type = DatabaseTypeOracle
		if err := parseOracleURL(cfg, parsedURL); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported database URL scheme: %s", parsedURL.Scheme)
	}

	log.Debug("Parsed database URL: type=%s", cfg.Type)
	return cfg, nil
}

// parsePostgresURL extracts PostgreSQL connection parameters from a URL
func parsePostgresURL(cfg *GormConfig, u *url.URL) error {
	cfg.Host = u.Hostname()
	cfg.Port = u.Port()
	if cfg.Port == "" {
		cfg.Port = "5432"
	}

	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	// Database name is the path without leading slash
	cfg.Database = strings.TrimPrefix(u.Path, "/")

	// Parse query parameters for sslmode
	query := u.Query()
	cfg.SSLMode = query.Get("sslmode")
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}

	return nil
}

// parseMySQLURL extracts MySQL connection parameters from a URL
func parseMySQLURL(cfg *GormConfig, u *url.URL) error {
	cfg.Host = u.Hostname()
	cfg.Port = u.Port()
	if cfg.Port == "" {
		cfg.Port = "3306"
	}

	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	// Database name is the path without leading slash
	cfg.Database = strings.TrimPrefix(u.Path, "/")

	return nil
}

// parseSQLServerURL extracts SQL Server connection parameters from a URL
func parseSQLServerURL(cfg *GormConfig, u *url.URL) error {
	cfg.Host = u.Hostname()
	cfg.Port = u.Port()
	if cfg.Port == "" {
		cfg.Port = "1433"
	}

	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	// SQL Server often passes database as query parameter
	query := u.Query()
	cfg.Database = query.Get("database")
	if cfg.Database == "" {
		// Fall back to path
		cfg.Database = strings.TrimPrefix(u.Path, "/")
	}

	return nil
}

// parseSQLiteURL extracts SQLite connection parameters from a URL
func parseSQLiteURL(cfg *GormConfig, u *url.URL) error {
	// SQLite URL can be:
	// - sqlite:///path/to/file.db (absolute path)
	// - sqlite://./relative/path.db (relative path)
	// - sqlite://:memory: (in-memory)

	path := u.Path
	if u.Host != "" {
		// Handle sqlite://./relative/path.db format
		path = u.Host + path
	}

	// Handle :memory: special case
	if path == "" && u.Opaque == ":memory:" {
		cfg.SQLitePath = ":memory:"
	} else if path == ":memory:" || strings.HasSuffix(u.String(), ":memory:") {
		cfg.SQLitePath = ":memory:"
	} else {
		cfg.SQLitePath = path
	}

	return nil
}

// parseOracleURL extracts Oracle connection parameters from a URL
// Supported formats:
//   - oracle://user:password@host:port/service_name - Easy Connect format
//   - oracle://user@tns_alias - TNS alias format (for OCI ADB with wallet)
//
// When using OCI Autonomous Database with a wallet:
//   - Use the TNS alias from wallet/tnsnames.ora (e.g., tmidb_tp)
//   - Password is provided via ORACLE_PASSWORD env var or included in URL
//   - Wallet location is set via database.oracle_wallet_location in config
func parseOracleURL(cfg *GormConfig, u *url.URL) error {
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	// If no password in URL, check ORACLE_PASSWORD environment variable
	// This allows keeping passwords out of config files for security
	if cfg.Password == "" {
		if envPassword := os.Getenv("ORACLE_PASSWORD"); envPassword != "" {
			cfg.Password = envPassword
		}
	}

	cfg.Host = u.Hostname()
	cfg.Port = u.Port()
	serviceName := strings.TrimPrefix(u.Path, "/")
	cfg.Database = serviceName

	// Determine if this is a TNS alias or Easy Connect format
	// TNS alias: oracle://user@tns_alias (no port specified, no service name in path)
	// Easy Connect: oracle://user:pass@host:port/service_name
	if cfg.Port == "" && serviceName == "" {
		// TNS alias format - host is actually the TNS alias
		// The alias will be resolved via tnsnames.ora in the wallet directory
		cfg.OracleConnectString = cfg.Host
	} else {
		// Easy Connect format - build host:port/service_name
		if cfg.Port == "" {
			cfg.Port = "1521"
		}
		cfg.OracleConnectString = fmt.Sprintf("%s:%s/%s", cfg.Host, cfg.Port, serviceName)
	}

	// Check for wallet_location in query params
	query := u.Query()
	if walletLoc := query.Get("wallet_location"); walletLoc != "" {
		cfg.OracleWalletLocation = walletLoc
	}

	return nil
}

// ParseRedisURL parses a Redis connection URL and returns connection parameters.
// Supported URL format: redis://[:password@]host:port[/db]
func ParseRedisURL(rawURL string) (host, port, password string, db int, err error) {
	if rawURL == "" {
		return "", "", "", 0, fmt.Errorf("redis URL is empty")
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("failed to parse redis URL: %w", err)
	}

	if parsedURL.Scheme != "redis" && parsedURL.Scheme != "rediss" {
		return "", "", "", 0, fmt.Errorf("invalid redis URL scheme: %s (expected 'redis' or 'rediss')", parsedURL.Scheme)
	}

	host = parsedURL.Hostname()
	port = parsedURL.Port()
	if port == "" {
		port = "6379"
	}

	if parsedURL.User != nil {
		password, _ = parsedURL.User.Password()
	}

	// Parse database number from path (e.g., /0, /1)
	db = 0
	if parsedURL.Path != "" && parsedURL.Path != "/" {
		dbStr := strings.TrimPrefix(parsedURL.Path, "/")
		if dbStr != "" {
			db, err = strconv.Atoi(dbStr)
			if err != nil {
				return "", "", "", 0, fmt.Errorf("invalid redis database number: %s", dbStr)
			}
		}
	}

	return host, port, password, db, nil
}

// OracleNamingStrategy converts all identifiers to uppercase for Oracle compatibility.
// Oracle folds unquoted identifiers to uppercase, so using uppercase names ensures
// that queries work correctly even when identifiers aren't quoted.
type OracleNamingStrategy struct {
	schema.NamingStrategy
}

// TableName converts table name to uppercase for Oracle
func (ns *OracleNamingStrategy) TableName(table string) string {
	return strings.ToUpper(ns.NamingStrategy.TableName(table))
}

// ColumnName converts column name to uppercase for Oracle
func (ns *OracleNamingStrategy) ColumnName(table, column string) string {
	return strings.ToUpper(ns.NamingStrategy.ColumnName(table, column))
}

// JoinTableName converts join table name to uppercase for Oracle
func (ns *OracleNamingStrategy) JoinTableName(joinTable string) string {
	return strings.ToUpper(ns.NamingStrategy.JoinTableName(joinTable))
}

// RelationshipFKName converts foreign key name to uppercase for Oracle
func (ns *OracleNamingStrategy) RelationshipFKName(rel schema.Relationship) string {
	return strings.ToUpper(ns.NamingStrategy.RelationshipFKName(rel))
}

// CheckerName converts checker constraint name to uppercase for Oracle
func (ns *OracleNamingStrategy) CheckerName(table, column string) string {
	return strings.ToUpper(ns.NamingStrategy.CheckerName(table, column))
}

// IndexName converts index name to uppercase for Oracle
func (ns *OracleNamingStrategy) IndexName(table, column string) string {
	return strings.ToUpper(ns.NamingStrategy.IndexName(table, column))
}

// UniqueName converts unique constraint name to uppercase for Oracle
func (ns *OracleNamingStrategy) UniqueName(table, column string) string {
	return strings.ToUpper(ns.NamingStrategy.UniqueName(table, column))
}

// NewGormDB creates a new GORM database connection based on configuration
func NewGormDB(cfg GormConfig) (*GormDB, error) {
	log := slogging.Get()
	log.Debug("Initializing GORM connection for database type: %s", cfg.Type)

	var dialector gorm.Dialector
	var dsn string

	switch cfg.Type {
	case DatabaseTypePostgres:
		// TimeZone=UTC ensures the session timezone is set to UTC, preventing issues
		// when the PostgreSQL server is configured for a non-UTC timezone
		dsn = fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.SSLMode,
		)
		dialector = postgres.Open(dsn)
		log.Debug("Using PostgreSQL dialector for %s:%s/%s", cfg.Host, cfg.Port, cfg.Database)

	case DatabaseTypeOracle:
		// Oracle support requires the 'oracle' build tag
		// Build with: go build -tags oracle
		dialector, _ = getOracleDialector(cfg)
		if dialector == nil {
			return nil, fmt.Errorf("oracle database support not compiled in; build with: go build -tags oracle")
		}
		log.Debug("Using Oracle dialector for %s", cfg.OracleConnectString)

	case DatabaseTypeMySQL:
		// MySQL DSN format: user:password@tcp(host:port)/dbname?parseTime=true
		// parseTime=true is required for proper time.Time scanning
		// loc=UTC ensures all timestamps are interpreted in UTC, preventing timezone offset issues
		// when the MySQL server or client system is in a non-UTC timezone
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=UTC&charset=utf8mb4&collation=utf8mb4_unicode_ci",
			cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
		dialector = mysql.Open(dsn)
		log.Debug("Using MySQL dialector for %s:%s/%s", cfg.Host, cfg.Port, cfg.Database)

	case DatabaseTypeSQLServer:
		// SQL Server DSN format: sqlserver://user:password@host:port?database=dbname
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s",
			cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
		dialector = sqlserver.Open(dsn)
		log.Debug("Using SQL Server dialector for %s:%s/%s", cfg.Host, cfg.Port, cfg.Database)

	case DatabaseTypeSQLite:
		// SQLite DSN is just the file path, or ":memory:" for in-memory database
		dsn = cfg.SQLitePath
		dialector = sqlite.Open(dsn)
		log.Debug("Using SQLite dialector for %s", cfg.SQLitePath)

	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}

	// Configure GORM
	prepareStmt := true
	gormConfig := &gorm.Config{
		Logger: newGormLogger(log),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
		PrepareStmt: prepareStmt,
	}

	// For Oracle, use uppercase naming strategy.
	// Oracle folds unquoted identifiers to uppercase, but the oracle-samples/gorm-oracle
	// driver doesn't consistently quote all identifiers (e.g., WHERE clause columns).
	// Using uppercase names ensures compatibility with Oracle's default behavior.
	if cfg.Type == DatabaseTypeOracle {
		gormConfig.NamingStrategy = &OracleNamingStrategy{
			NamingStrategy: schema.NamingStrategy{},
		}
		// Also set the models package flag so TableName() methods return uppercase
		models.UseUppercaseTableNames = true
		log.Debug("Using Oracle uppercase naming strategy")
	}

	// Open database connection
	log.Debug("Opening GORM database connection")
	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		log.Error("Failed to open GORM connection: %v", err)
		return nil, fmt.Errorf("failed to open gorm connection: %w", err)
	}

	// Get underlying sql.DB to configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		log.Error("Failed to get underlying sql.DB: %v", err)
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Set connection pool parameters (configurable, with defaults)
	// Use shorter max lifetime (4 min) to proactively recycle connections before they go stale
	// Use 30s idle timeout to match Heroku Postgres which terminates idle connections after ~30s
	maxOpenConns := cfg.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = 10 // default
	}
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 2 // default
	}
	connMaxLifetime := cfg.ConnMaxLifetime
	if connMaxLifetime <= 0 {
		connMaxLifetime = 240 // default: 4 minutes in seconds
	}
	connMaxIdleTime := cfg.ConnMaxIdleTime
	if connMaxIdleTime <= 0 {
		connMaxIdleTime = 30 // default: 30 seconds
	}

	log.Debug("Setting GORM connection pool parameters: maxOpen=%d, maxIdle=%d, maxLifetime=%ds, maxIdleTime=%ds",
		maxOpenConns, maxIdleConns, connMaxLifetime, connMaxIdleTime)
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(connMaxLifetime) * time.Second)
	sqlDB.SetConnMaxIdleTime(time.Duration(connMaxIdleTime) * time.Second)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Debug("Testing GORM connection with ping")
	if err := sqlDB.PingContext(ctx); err != nil {
		log.Error("Failed to ping database: %v", err)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	log.Debug("GORM connection established successfully")

	// Configure session timezone for databases that require it
	if err := configureSessionTimezone(db, cfg.Type, log); err != nil {
		log.Error("Failed to configure session timezone: %v", err)
		return nil, fmt.Errorf("failed to configure session timezone: %w", err)
	}

	return &GormDB{
		db:        db,
		cfg:       cfg,
		dialector: dialector,
	}, nil
}

// Close closes the database connection
func (g *GormDB) Close() error {
	log := slogging.Get()
	log.Debug("Closing GORM connection")

	sqlDB, err := g.db.DB()
	if err != nil {
		log.Error("Failed to get underlying sql.DB for close: %v", err)
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		log.Error("Error closing GORM connection: %v", err)
		return fmt.Errorf("error closing database connection: %w", err)
	}

	log.Debug("GORM connection closed successfully")
	return nil
}

// DB returns the GORM database instance
func (g *GormDB) DB() *gorm.DB {
	return g.db
}

// DatabaseType returns the type of database (postgres or oracle)
func (g *GormDB) DatabaseType() DatabaseType {
	return g.cfg.Type
}

// Ping checks if the database connection is alive
func (g *GormDB) Ping(ctx context.Context) error {
	log := slogging.Get()
	log.Debug("Pinging GORM connection")

	sqlDB, err := g.db.DB()
	if err != nil {
		log.Error("Failed to get underlying sql.DB for ping: %v", err)
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		log.Error("GORM ping failed: %v", err)
		return err
	}

	log.Debug("GORM ping successful")
	return nil
}

// LogStats logs statistics about the database connection pool
func (g *GormDB) LogStats() {
	log := slogging.Get()

	sqlDB, err := g.db.DB()
	if err != nil {
		log.Error("Failed to get underlying sql.DB for stats: %v", err)
		return
	}

	stats := sqlDB.Stats()
	log.Debug("GORM connection pool stats: open=%d, inUse=%d, idle=%d, waitCount=%d, waitDuration=%s, maxIdleClosed=%d, maxLifetimeClosed=%d",
		stats.OpenConnections,
		stats.InUse,
		stats.Idle,
		stats.WaitCount,
		stats.WaitDuration,
		stats.MaxIdleClosed,
		stats.MaxLifetimeClosed,
	)
}

// AutoMigrate runs GORM auto-migration for the given models.
// For Oracle, models are migrated individually so that a benign ORA-01442
// error on one model does not prevent migration of subsequent models.
func (g *GormDB) AutoMigrate(models ...interface{}) error {
	log := slogging.Get()
	log.Debug("Running GORM auto-migration for %d models", len(models))

	if g.cfg.Type == DatabaseTypeOracle {
		if err := g.autoMigrateOracle(models...); err != nil {
			return err
		}
	} else {
		if err := g.db.AutoMigrate(models...); err != nil {
			log.Error("GORM auto-migration failed: %v", err)
			return fmt.Errorf("auto-migration failed: %w", err)
		}
	}

	log.Debug("GORM auto-migration completed successfully")

	// Drop stale FK constraints that were removed from GORM models.
	// GORM AutoMigrate does not drop FK constraints when a relationship is removed
	// from a model struct, so we must do it explicitly.
	dropStaleForeignKeys(g.db)

	return nil
}

// autoMigrateOracle migrates each model individually to work around Oracle-specific
// GORM issues. Oracle raises benign errors during migration when the schema is
// already in the desired state:
//   - ORA-01442: "column to be modified to NOT NULL is already NOT NULL"
//   - ORA-01408: "such column list already indexed"
//   - ORA-01430: "column being added already exists in table"
//
// These errors can fire on referenced FK tables (e.g., USERS) before GORM creates
// the model's own table, preventing table creation even with
// DisableForeignKeyConstraintWhenMigrating enabled (GORM still validates referenced
// table columns). When a benign error occurs and the model's table doesn't exist,
// we fall back to CreateTable to ensure the table is created, then retry
// AutoMigrate for any additional schema changes (indexes, constraints).
func (g *GormDB) autoMigrateOracle(models ...interface{}) error {
	log := slogging.Get()
	for _, model := range models {
		modelName := reflect.TypeOf(model).Elem().Name()
		if err := g.db.AutoMigrate(model); err != nil {
			if !isOracleBenignMigrationError(err) {
				log.Error("GORM auto-migration failed for model %s: %v", modelName, err)
				return fmt.Errorf("auto-migration failed for %s: %w", modelName, err)
			}
			// Benign error — check if the table actually exists
			if g.db.Migrator().HasTable(model) {
				log.Debug("Oracle migration warning for %s: checking for missing columns", modelName)
				if colErr := g.addMissingColumnsOracle(model, modelName); colErr != nil {
					return colErr
				}
				continue
			}
			// Table doesn't exist — the benign error was from a referenced FK table.
			// Use CreateTable to create just this table, then retry AutoMigrate for
			// indexes and constraints.
			log.Info("Oracle migration for %s: creating missing table after benign FK error", modelName)
			if createErr := g.db.Migrator().CreateTable(model); createErr != nil {
				log.Error("Failed to create table for model %s: %v", modelName, createErr)
				return fmt.Errorf("failed to create table for %s: %w", modelName, createErr)
			}
			// Retry AutoMigrate to add indexes and constraints
			if retryErr := g.db.AutoMigrate(model); retryErr != nil {
				if isOracleBenignMigrationError(retryErr) {
					log.Debug("Oracle migration warning for %s on retry (ignored): schema already in desired state", modelName)
					continue
				}
				log.Error("GORM auto-migration failed for model %s on retry: %v", modelName, retryErr)
				return fmt.Errorf("auto-migration failed for %s on retry: %w", modelName, retryErr)
			}
			log.Debug("Migrated model %s (table created, then auto-migrated)", modelName)
			continue
		}
		log.Debug("Migrated model %s", modelName)
	}
	return nil
}

// dropStaleForeignKeys drops FK constraints that were removed from GORM model
// structs. GORM AutoMigrate only adds/modifies — it never drops constraints.
// This function is idempotent; it silently skips constraints that don't exist.
func dropStaleForeignKeys(db *gorm.DB) {
	log := slogging.Get()

	// FK constraints removed because survey templates are system-owned,
	// not associated with a specific user.
	staleConstraints := []struct {
		table      string
		constraint string
	}{
		{"survey_templates", "fk_survey_templates_created_by"},
		{"survey_template_versions", "fk_survey_template_versions_created_by"},
	}

	for _, c := range staleConstraints {
		if db.Migrator().HasTable(c.table) && db.Migrator().HasConstraint(c.table, c.constraint) {
			if err := db.Migrator().DropConstraint(c.table, c.constraint); err != nil {
				log.Warn("Failed to drop stale FK constraint %s on %s: %v", c.constraint, c.table, err)
			} else {
				log.Info("Dropped stale FK constraint %s on %s", c.constraint, c.table)
			}
		}
	}
}

// addMissingColumnsOracle checks for and adds any columns that exist in the GORM model
// but are missing from the database table. This handles the case where a benign Oracle
// error (e.g., ORA-01442) during AutoMigrate's MigrateColumn step causes GORM to abort
// the model before reaching AddColumn for new columns.
func (g *GormDB) addMissingColumnsOracle(model interface{}, modelName string) error {
	log := slogging.Get()

	// Get existing columns from the database
	columnTypes, err := g.db.Migrator().ColumnTypes(model)
	if err != nil {
		return fmt.Errorf("failed to get column types for %s: %w", modelName, err)
	}

	existingColumns := make(map[string]bool, len(columnTypes))
	for _, col := range columnTypes {
		existingColumns[strings.ToUpper(col.Name())] = true
	}

	// Parse the model's GORM schema to get expected DB column names.
	// Uses OracleNamingStrategy to produce uppercase column names matching Oracle.
	parsedSchema, parseErr := schema.Parse(model, &sync.Map{}, &OracleNamingStrategy{})
	if parseErr != nil {
		return fmt.Errorf("failed to parse schema for %s: %w", modelName, parseErr)
	}

	// Add any missing columns
	for _, dbName := range parsedSchema.DBNames {
		if !existingColumns[strings.ToUpper(dbName)] {
			field := parsedSchema.FieldsByDBName[dbName]
			log.Info("Adding missing column %s to %s", dbName, modelName)
			if addErr := g.db.Migrator().AddColumn(model, field.Name); addErr != nil {
				if isOracleBenignMigrationError(addErr) {
					log.Debug("Benign error adding column %s to %s (ignored)", dbName, modelName)
					continue
				}
				return fmt.Errorf("failed to add column %s to %s: %w", dbName, modelName, addErr)
			}
		}
	}

	return nil
}

// isOracleBenignMigrationError checks if the error is a benign Oracle migration error
// that indicates the schema is already in the desired state:
//   - ORA-01442: "column to be modified to NOT NULL is already NOT NULL"
//   - ORA-01408: "such column list already indexed"
//   - ORA-01430: "column being added already exists in table"
func isOracleBenignMigrationError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "ORA-01442") ||
		strings.Contains(errStr, "ORA-01408") ||
		strings.Contains(errStr, "ORA-01430")
}

// gormLogger adapts our slogging to GORM's logger interface
type gormLogger struct {
	log *slogging.Logger
}

func newGormLogger(log *slogging.Logger) logger.Interface {
	return &gormLogger{log: log}
}

func (l *gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return l
}

func (l *gormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	l.log.Info(msg, data...)
}

func (l *gormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	l.log.Warn(msg, data...)
}

func (l *gormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	l.log.Error(msg, data...)
}

func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()

	if err != nil {
		l.log.Error("GORM query error: %v [%s] (%d rows, %s)", err, sql, rows, elapsed)
	} else {
		l.log.Debug("GORM query: %s (%d rows, %s)", sql, rows, elapsed)
	}
}

// configureSessionTimezone sets the session timezone to UTC for databases that require it.
// This ensures consistent timestamp handling regardless of the database server's timezone.
//
// Note on connection pooling: This function runs once at connection initialization.
// For Oracle, the session timezone is set per-session, so new connections from the pool
// will inherit the server's default timezone. However, since GORM's NowFunc is configured
// to use UTC and Go's time.Time is timezone-aware, timestamps are handled correctly
// at the application level. The session timezone setting primarily affects:
// - SYSDATE/SYSTIMESTAMP functions in Oracle
// - CURRENT_TIMESTAMP in SQL Server
// - Any database-side date arithmetic
func configureSessionTimezone(db *gorm.DB, dbType DatabaseType, log *slogging.Logger) error {
	switch dbType {
	case DatabaseTypeOracle:
		// Set Oracle session timezone to UTC
		// This affects SYSDATE, SYSTIMESTAMP, and date arithmetic in Oracle
		// Note: This only affects the current session; new pooled connections may need reconfiguration
		log.Debug("Setting Oracle session timezone to UTC")
		if err := db.Exec("ALTER SESSION SET TIME_ZONE = '+00:00'").Error; err != nil {
			return fmt.Errorf("failed to set Oracle session timezone: %w", err)
		}
		log.Debug("Oracle session timezone set to UTC successfully")

	case DatabaseTypeSQLServer:
		// SQL Server doesn't have a session timezone setting like other databases.
		// It uses DATETIME2 which stores timestamps without timezone information.
		// The application layer (Go's time.Time with GORM's UTC NowFunc) handles
		// timezone conversion. No additional configuration needed.
		log.Debug("SQL Server: no session timezone configuration needed (using DATETIME2 with application-level UTC)")

	case DatabaseTypePostgres, DatabaseTypeMySQL, DatabaseTypeSQLite:
		// PostgreSQL: TimeZone is set in the DSN connection string
		// MySQL: loc=UTC is set in the DSN connection string
		// SQLite: Stores timestamps as TEXT, no timezone issues
		log.Debug("Session timezone already configured via DSN or not applicable for %s", dbType)
	}

	return nil
}

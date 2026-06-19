package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// sqliteMemoryPath is the special SQLite path for in-memory databases
const sqliteMemoryPath = ":memory:"

// DatabaseType represents the type of database
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: string type enumerating supported database backends (pure)
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
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: configuration struct for a GORM database connection parsed from a URL (pure)
type GormConfig struct {
	Type DatabaseType // Database type (extracted from URL scheme)

	// Unified connection parameters (extracted from DATABASE_URL)
	Host     string // Database host
	Port     string // Database port
	User     string // Database username
	Password string
	Database string // Database name
	SSLMode  string // SSL mode (PostgreSQL: disable/require/prefer)

	// SQLite-specific
	SQLitePath string // File path or sqliteMemoryPath for in-memory database

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
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: GORM database connection wrapper supporting multiple SQL backends (pure)
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
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: parse a multi-dialect database URL into a GormConfig (pure)
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
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: extract PostgreSQL connection parameters from a parsed URL into GormConfig (pure)
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
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: extract MySQL connection parameters from a parsed URL into GormConfig (pure)
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
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: extract SQL Server connection parameters from a parsed URL into GormConfig (pure)
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
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: extract SQLite file path or in-memory sentinel from a parsed URL into GormConfig (pure)
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
	switch {
	case path == "" && u.Opaque == sqliteMemoryPath:
		cfg.SQLitePath = sqliteMemoryPath
	case path == sqliteMemoryPath || strings.HasSuffix(u.String(), sqliteMemoryPath):
		cfg.SQLitePath = sqliteMemoryPath
	default:
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
// SEM@b650823b66084bdf6c24237cb2cf375aff547400: extract Oracle Easy Connect or TNS alias parameters from a parsed URL into GormConfig (pure)
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
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: parse a Redis connection URL and return host, port, password, and database index (pure)
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
// SEM@0953d9ec7f7a4717796566e1b4379a976404b07e: GORM naming strategy that uppercases all identifiers for Oracle compatibility (pure)
type OracleNamingStrategy struct {
	schema.NamingStrategy
}

// TableName converts table name to uppercase for Oracle
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: convert a table name to uppercase for Oracle (pure)
func (ns *OracleNamingStrategy) TableName(table string) string {
	return strings.ToUpper(ns.NamingStrategy.TableName(table))
}

// ColumnName converts column name to uppercase for Oracle
// SEM@0953d9ec7f7a4717796566e1b4379a976404b07e: convert a column name to uppercase for Oracle (pure)
func (ns *OracleNamingStrategy) ColumnName(table, column string) string {
	return strings.ToUpper(ns.NamingStrategy.ColumnName(table, column))
}

// JoinTableName converts join table name to uppercase for Oracle
// SEM@0953d9ec7f7a4717796566e1b4379a976404b07e: convert a join table name to uppercase for Oracle (pure)
func (ns *OracleNamingStrategy) JoinTableName(joinTable string) string {
	return strings.ToUpper(ns.NamingStrategy.JoinTableName(joinTable))
}

// RelationshipFKName converts foreign key name to uppercase for Oracle
// SEM@0953d9ec7f7a4717796566e1b4379a976404b07e: convert a foreign key constraint name to uppercase for Oracle (pure)
func (ns *OracleNamingStrategy) RelationshipFKName(rel schema.Relationship) string {
	return strings.ToUpper(ns.NamingStrategy.RelationshipFKName(rel))
}

// CheckerName converts checker constraint name to uppercase for Oracle
// SEM@0953d9ec7f7a4717796566e1b4379a976404b07e: convert a check constraint name to uppercase for Oracle (pure)
func (ns *OracleNamingStrategy) CheckerName(table, column string) string {
	return strings.ToUpper(ns.NamingStrategy.CheckerName(table, column))
}

// IndexName converts index name to uppercase for Oracle
// SEM@0953d9ec7f7a4717796566e1b4379a976404b07e: convert an index name to uppercase for Oracle (pure)
func (ns *OracleNamingStrategy) IndexName(table, column string) string {
	return strings.ToUpper(ns.NamingStrategy.IndexName(table, column))
}

// UniqueName converts unique constraint name to uppercase for Oracle
// SEM@0953d9ec7f7a4717796566e1b4379a976404b07e: convert a unique constraint name to uppercase for Oracle (pure)
func (ns *OracleNamingStrategy) UniqueName(table, column string) string {
	return strings.ToUpper(ns.NamingStrategy.UniqueName(table, column))
}

// NewGormDB creates a new GORM database connection based on configuration
// SEM@9be9de48236704afd7be7c8f4e5602ce2235739f: connect to a database via GORM with pooling, OTel tracing, and UTC session timezone
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
		// MySQL support requires the 'mysql' build tag
		// Build with: go build -tags mysql
		dialector = getMySQLDialector(cfg)
		if dialector == nil {
			return nil, fmt.Errorf("mysql database support not compiled in; build with: go build -tags mysql")
		}
		log.Debug("Using MySQL dialector for %s:%s/%s", cfg.Host, cfg.Port, cfg.Database)

	case DatabaseTypeSQLServer:
		// SQL Server support requires the 'sqlserver' build tag
		// Build with: go build -tags sqlserver
		dialector = getSQLServerDialector(cfg)
		if dialector == nil {
			return nil, fmt.Errorf("sqlserver database support not compiled in; build with: go build -tags sqlserver")
		}
		log.Debug("Using SQL Server dialector for %s:%s/%s", cfg.Host, cfg.Port, cfg.Database)

	case DatabaseTypeSQLite:
		// SQLite DSN is just the file path, or sqliteMemoryPath for in-memory database
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

	// Register OpenTelemetry GORM plugin for query tracing
	if err := db.Use(otelgorm.NewPlugin(
		otelgorm.WithDBName(cfg.Database),
		otelgorm.WithoutQueryVariables(),
	)); err != nil {
		log.Warn("Failed to register OTel GORM plugin (tracing disabled for DB): %v", err)
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
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: close the underlying database connection pool (mutates shared state)
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
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: return the raw GORM database instance (pure)
func (g *GormDB) DB() *gorm.DB {
	return g.db
}

// DatabaseType returns the type of database (postgres or oracle)
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: return the configured database backend type for this connection (pure)
func (g *GormDB) DatabaseType() DatabaseType {
	return g.cfg.Type
}

// Ping checks if the database connection is alive
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: validate the GORM database connection is alive via ping (reads DB)
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
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: log connection pool statistics to structured debug output (reads DB)
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
// SEM@ba7ef88caa84239c54ef87465cd9a14f01f61e3d: apply additive schema migrations for all registered models (mutates shared state)
func (g *GormDB) AutoMigrate(models ...any) error {
	log := slogging.Get()
	log.Debug("Running GORM auto-migration for %d models", len(models))

	// All dialects, including Oracle, use a single batched AutoMigrate call.
	// Passing every model in one call lets GORM topologically sort them and
	// create all tables before resolving foreign-key constraints across the
	// set. TMI uses a single fresh-schema baseline (#412): there is no
	// version-to-version ALTER path.
	//
	// Oracle previously needed a benign-ORA-01442 swallow here because
	// gorm-oracle re-issued redundant ALTER ... MODIFY on existing columns and
	// the batched call aborts on the first error — which silently dropped any
	// genuinely-new column added later in the batch (#474). That is now fixed
	// at the source: the Oracle dialector uses an additive migrator whose
	// MigrateColumn is a no-op (see auth/db/gorm_oracle.go), so existing
	// columns are never ALTERed and no benign error needs swallowing. Real
	// migration errors now surface on every dialect.
	if err := g.db.AutoMigrate(models...); err != nil {
		log.Error("GORM auto-migration failed: %v", err)
		return fmt.Errorf("auto-migration failed: %w", err)
	}
	log.Debug("GORM auto-migration completed successfully")
	return nil
}

// gormLogger adapts our slogging to GORM's logger interface
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: GORM logger adapter that routes database log events to the structured logger (pure)
type gormLogger struct {
	log *slogging.Logger
}

// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: build a GORM logger.Interface backed by the structured logger (pure)
func newGormLogger(log *slogging.Logger) logger.Interface {
	return &gormLogger{log: log}
}

// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: return the logger unchanged; log level changes are no-ops (pure)
func (l *gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	return l
}

// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: dispatch a GORM info message to the structured logger at info level (pure)
func (l *gormLogger) Info(ctx context.Context, msg string, data ...any) {
	l.log.Info(msg, data...)
}

// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: dispatch a GORM warning message to the structured logger at warn level (pure)
func (l *gormLogger) Warn(ctx context.Context, msg string, data ...any) {
	l.log.Warn(msg, data...)
}

// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: dispatch a GORM error message to the structured logger at error level (pure)
func (l *gormLogger) Error(ctx context.Context, msg string, data ...any) {
	l.log.Error(msg, data...)
}

// SEM@7b44dd28820ffc230e89ff205b4e042638b5d35c: log a completed GORM query with elapsed time and error status (pure)
func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			l.log.Debug("GORM query: record not found [%s] (%d rows, %s)", sql, rows, elapsed)
		} else {
			l.log.Error("GORM query error: %v [%s] (%d rows, %s)", err, sql, rows, elapsed)
		}
	} else {
		l.log.Debug("GORM query: %s (%d rows, %s)", sql, rows, elapsed)
	}
}

// configureSessionTimezone sets the session timezone to UTC for databases that require it.
// This ensures consistent timestamp handling regardless of the database server's timezone.
//
// Note on connection pooling: a one-shot ALTER SESSION here would only affect a single
// connection from the pool, leaving every other pooled session on the server's default
// timezone (issue #459). Per-pool enforcement is therefore done in the DSN, not here:
//   - Oracle: godror's onInit runs "ALTER SESSION SET TIME_ZONE = '+00:00'" on every new
//     pooled session (see oracleSessionInitParams in gorm_oracle.go).
//   - PostgreSQL/MySQL: TimeZone/loc=UTC is set in the DSN connection string.
//
// This function now only handles the per-connection cases that have no DSN-level lever.
// SEM@22b322e46521c0753cc08719412daba7aeb05196: apply per-dialect session timezone settings after connection open (pure)
func configureSessionTimezone(db *gorm.DB, dbType DatabaseType, log *slogging.Logger) error {
	switch dbType {
	case DatabaseTypeOracle:
		// Oracle session timezone is enforced for every pooled connection via the
		// godror onInit DSN parameter (oracleSessionInitParams). Doing it here would
		// only cover one pooled session, so this is intentionally a no-op.
		log.Debug("Oracle session timezone enforced per-pool via DSN onInit (issue #459); no per-connection ALTER needed")

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

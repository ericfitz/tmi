package db

import (
	"context"
	"fmt"
	"strings"
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

// GormConfig holds the configuration for GORM database connection
type GormConfig struct {
	Type DatabaseType

	// PostgreSQL configuration
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDatabase string
	PostgresSSLMode  string

	// Oracle configuration
	OracleUser           string
	OraclePassword       string
	OracleConnectString  string
	OracleWalletLocation string

	// MySQL configuration
	MySQLHost     string
	MySQLPort     string
	MySQLUser     string
	MySQLPassword string
	MySQLDatabase string

	// SQL Server configuration
	SQLServerHost     string
	SQLServerPort     string
	SQLServerUser     string
	SQLServerPassword string
	SQLServerDatabase string

	// SQLite configuration
	SQLitePath string // File path or ":memory:" for in-memory database
}

// GormDB represents a GORM database connection that works with PostgreSQL, Oracle, MySQL, SQL Server, and SQLite
type GormDB struct {
	db        *gorm.DB
	cfg       GormConfig
	dialector gorm.Dialector
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
			cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresUser,
			cfg.PostgresPassword, cfg.PostgresDatabase, cfg.PostgresSSLMode,
		)
		dialector = postgres.Open(dsn)
		log.Debug("Using PostgreSQL dialector for %s:%s/%s", cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDatabase)

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
			cfg.MySQLUser, cfg.MySQLPassword, cfg.MySQLHost, cfg.MySQLPort, cfg.MySQLDatabase)
		dialector = mysql.Open(dsn)
		log.Debug("Using MySQL dialector for %s:%s/%s", cfg.MySQLHost, cfg.MySQLPort, cfg.MySQLDatabase)

	case DatabaseTypeSQLServer:
		// SQL Server DSN format: sqlserver://user:password@host:port?database=dbname
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s",
			cfg.SQLServerUser, cfg.SQLServerPassword, cfg.SQLServerHost, cfg.SQLServerPort, cfg.SQLServerDatabase)
		dialector = sqlserver.Open(dsn)
		log.Debug("Using SQL Server dialector for %s:%s/%s", cfg.SQLServerHost, cfg.SQLServerPort, cfg.SQLServerDatabase)

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

	// Set connection pool parameters (same as existing PostgresDB)
	// Use shorter max lifetime (4 min) to proactively recycle connections before they go stale
	// Use 30s idle timeout to match Heroku Postgres which terminates idle connections after ~30s
	log.Debug("Setting GORM connection pool parameters: maxOpen=10, maxIdle=2, maxLifetime=4m, maxIdleTime=30s")
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(4 * time.Minute)
	sqlDB.SetConnMaxIdleTime(30 * time.Second)

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

// AutoMigrate runs GORM auto-migration for the given models
func (g *GormDB) AutoMigrate(models ...interface{}) error {
	log := slogging.Get()
	log.Debug("Running GORM auto-migration for %d models", len(models))

	if err := g.db.AutoMigrate(models...); err != nil {
		// For Oracle, ignore ORA-01442 "column to be modified to NOT NULL is already NOT NULL"
		// This error occurs when GORM tries to re-apply NOT NULL constraints during migration
		// on columns that are already NOT NULL. This is safe to ignore as it means
		// the schema is already in the desired state.
		if g.cfg.Type == DatabaseTypeOracle && isOracleAlreadyNotNullError(err) {
			log.Warn("Oracle migration warning (ignored): column already NOT NULL - schema is in desired state")
			log.Debug("GORM auto-migration completed with acceptable Oracle warnings")
			return nil
		}
		log.Error("GORM auto-migration failed: %v", err)
		return fmt.Errorf("auto-migration failed: %w", err)
	}

	log.Debug("GORM auto-migration completed successfully")
	return nil
}

// isOracleAlreadyNotNullError checks if the error is Oracle's ORA-01442
// "column to be modified to NOT NULL is already NOT NULL"
func isOracleAlreadyNotNullError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "ORA-01442")
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

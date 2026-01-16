// Package testdb provides direct database access for integration tests.
// It allows tests to set up fixtures, verify data, and clean up without going through the API.
package testdb

import (
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/config"
	"gorm.io/gorm"
)

// TestDB provides direct database access for integration tests
type TestDB struct {
	gormDB *db.GormDB
	db     *gorm.DB
	config *config.Config
}

// New creates a TestDB from a config file path
func New(configFile string) (*TestDB, error) {
	cfg, err := config.Load(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return NewFromConfig(cfg)
}

// NewFromConfig creates a TestDB from an existing config
func NewFromConfig(cfg *config.Config) (*TestDB, error) {
	gormCfg := buildGormConfig(cfg)

	gormDB, err := db.NewGormDB(gormCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create GORM DB: %w", err)
	}

	return &TestDB{
		gormDB: gormDB,
		db:     gormDB.DB(),
		config: cfg,
	}, nil
}

// buildGormConfig converts application config to GORM config
func buildGormConfig(cfg *config.Config) db.GormConfig {
	return db.GormConfig{
		Type:                 db.DatabaseType(cfg.Database.Type),
		PostgresHost:         cfg.Database.Postgres.Host,
		PostgresPort:         cfg.Database.Postgres.Port,
		PostgresUser:         cfg.Database.Postgres.User,
		PostgresPassword:     cfg.Database.Postgres.Password,
		PostgresDatabase:     cfg.Database.Postgres.Database,
		PostgresSSLMode:      cfg.Database.Postgres.SSLMode,
		OracleUser:           cfg.Database.Oracle.User,
		OraclePassword:       cfg.Database.Oracle.Password,
		OracleConnectString:  cfg.Database.Oracle.ConnectString,
		OracleWalletLocation: cfg.Database.Oracle.WalletLocation,
		MySQLHost:            cfg.Database.MySQL.Host,
		MySQLPort:            cfg.Database.MySQL.Port,
		MySQLUser:            cfg.Database.MySQL.User,
		MySQLPassword:        cfg.Database.MySQL.Password,
		MySQLDatabase:        cfg.Database.MySQL.Database,
		SQLServerHost:        cfg.Database.SQLServer.Host,
		SQLServerPort:        cfg.Database.SQLServer.Port,
		SQLServerUser:        cfg.Database.SQLServer.User,
		SQLServerPassword:    cfg.Database.SQLServer.Password,
		SQLServerDatabase:    cfg.Database.SQLServer.Database,
		SQLitePath:           cfg.Database.SQLite.Path,
	}
}

// DB returns the underlying GORM DB for direct queries
func (t *TestDB) DB() *gorm.DB {
	return t.db
}

// Config returns the loaded configuration
func (t *TestDB) Config() *config.Config {
	return t.config
}

// DialectName returns the name of the database dialect
func (t *TestDB) DialectName() string {
	return t.db.Dialector.Name()
}

// Close closes the database connection
func (t *TestDB) Close() error {
	return t.gormDB.Close()
}

// AutoMigrate runs GORM AutoMigrate for all models
func (t *TestDB) AutoMigrate() error {
	return t.db.AutoMigrate(
		&models.User{},
		&models.RefreshTokenRecord{},
		&models.ClientCredential{},
		&models.ThreatModel{},
		&models.Diagram{},
		&models.Asset{},
		&models.Threat{},
		&models.Group{},
		&models.ThreatModelAccess{},
		&models.Document{},
		&models.Note{},
		&models.Repository{},
		&models.Metadata{},
		&models.CollaborationSession{},
		&models.SessionParticipant{},
		&models.WebhookSubscription{},
		&models.WebhookDelivery{},
		&models.WebhookQuota{},
		&models.WebhookURLDenyList{},
		&models.Administrator{},
		&models.Addon{},
		&models.AddonInvocationQuota{},
		&models.UserAPIQuota{},
		&models.GroupMember{},
	)
}

// --- User Operations ---

// CreateUser creates a test user and returns it
func (t *TestDB) CreateUser(user *models.User) error {
	return t.db.Create(user).Error
}

// GetUser retrieves a user by internal UUID
func (t *TestDB) GetUser(internalUUID string) (*models.User, error) {
	var user models.User
	if err := t.db.First(&user, "internal_uuid = ?", internalUUID).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail retrieves a user by email
func (t *TestDB) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	if err := t.db.First(&user, "email = ?", email).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// DeleteUser deletes a user by internal UUID
func (t *TestDB) DeleteUser(internalUUID string) error {
	return t.db.Delete(&models.User{}, "internal_uuid = ?", internalUUID).Error
}

// --- ThreatModel Operations ---

// CreateThreatModel creates a test threat model
func (t *TestDB) CreateThreatModel(tm *models.ThreatModel) error {
	return t.db.Create(tm).Error
}

// GetThreatModel retrieves a threat model by ID
func (t *TestDB) GetThreatModel(id string) (*models.ThreatModel, error) {
	var tm models.ThreatModel
	if err := t.db.First(&tm, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &tm, nil
}

// UpdateThreatModel updates a threat model
func (t *TestDB) UpdateThreatModel(tm *models.ThreatModel) error {
	return t.db.Save(tm).Error
}

// DeleteThreatModel deletes a threat model by ID
func (t *TestDB) DeleteThreatModel(id string) error {
	return t.db.Delete(&models.ThreatModel{}, "id = ?", id).Error
}

// ListThreatModelsByOwner lists threat models owned by a user
func (t *TestDB) ListThreatModelsByOwner(ownerInternalUUID string) ([]models.ThreatModel, error) {
	var tms []models.ThreatModel
	if err := t.db.Where("owner_internal_uuid = ?", ownerInternalUUID).Find(&tms).Error; err != nil {
		return nil, err
	}
	return tms, nil
}

// --- Diagram Operations ---

// CreateDiagram creates a test diagram
func (t *TestDB) CreateDiagram(d *models.Diagram) error {
	return t.db.Create(d).Error
}

// GetDiagram retrieves a diagram by ID
func (t *TestDB) GetDiagram(id string) (*models.Diagram, error) {
	var d models.Diagram
	if err := t.db.First(&d, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

// DeleteDiagram deletes a diagram by ID
func (t *TestDB) DeleteDiagram(id string) error {
	return t.db.Delete(&models.Diagram{}, "id = ?", id).Error
}

// ListDiagramsByThreatModel lists diagrams for a threat model
func (t *TestDB) ListDiagramsByThreatModel(threatModelID string) ([]models.Diagram, error) {
	var diagrams []models.Diagram
	if err := t.db.Where("threat_model_id = ?", threatModelID).Find(&diagrams).Error; err != nil {
		return nil, err
	}
	return diagrams, nil
}

// --- Threat Operations ---

// CreateThreat creates a test threat
func (t *TestDB) CreateThreat(threat *models.Threat) error {
	return t.db.Create(threat).Error
}

// GetThreat retrieves a threat by ID
func (t *TestDB) GetThreat(id string) (*models.Threat, error) {
	var threat models.Threat
	if err := t.db.First(&threat, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &threat, nil
}

// DeleteThreat deletes a threat by ID
func (t *TestDB) DeleteThreat(id string) error {
	return t.db.Delete(&models.Threat{}, "id = ?", id).Error
}

// --- Cleanup Operations ---

// CleanupByPrefix deletes all entities with names matching a prefix
// Cleans up in dependency order (children first)
func (t *TestDB) CleanupByPrefix(prefix string) error {
	pattern := prefix + "%"

	// Clean up threats
	if err := t.db.Where("title LIKE ?", pattern).Delete(&models.Threat{}).Error; err != nil {
		return fmt.Errorf("failed to delete threats: %w", err)
	}

	// Clean up diagrams
	if err := t.db.Where("name LIKE ?", pattern).Delete(&models.Diagram{}).Error; err != nil {
		return fmt.Errorf("failed to delete diagrams: %w", err)
	}

	// Clean up documents
	if err := t.db.Where("name LIKE ?", pattern).Delete(&models.Document{}).Error; err != nil {
		return fmt.Errorf("failed to delete documents: %w", err)
	}

	// Clean up notes
	if err := t.db.Where("title LIKE ?", pattern).Delete(&models.Note{}).Error; err != nil {
		return fmt.Errorf("failed to delete notes: %w", err)
	}

	// Clean up assets
	if err := t.db.Where("name LIKE ?", pattern).Delete(&models.Asset{}).Error; err != nil {
		return fmt.Errorf("failed to delete assets: %w", err)
	}

	// Clean up threat models
	if err := t.db.Where("name LIKE ?", pattern).Delete(&models.ThreatModel{}).Error; err != nil {
		return fmt.Errorf("failed to delete threat models: %w", err)
	}

	// Clean up users
	if err := t.db.Where("name LIKE ?", pattern).Delete(&models.User{}).Error; err != nil {
		return fmt.Errorf("failed to delete users: %w", err)
	}

	return nil
}

// Truncate removes all data from specified tables
// WARNING: Use with caution - this deletes all data
func (t *TestDB) Truncate(tables ...string) error {
	dialect := t.db.Dialector.Name()
	for _, table := range tables {
		var sql string
		switch dialect {
		case "postgres":
			sql = fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)
		case "mysql":
			// MySQL requires disabling foreign key checks for truncate
			if err := t.db.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
				return fmt.Errorf("failed to disable foreign key checks: %w", err)
			}
			sql = fmt.Sprintf("TRUNCATE TABLE %s", table)
		case "sqlserver":
			sql = fmt.Sprintf("TRUNCATE TABLE %s", table)
		case "sqlite":
			// SQLite has no TRUNCATE
			sql = fmt.Sprintf("DELETE FROM %s", table)
		case "oracle":
			sql = fmt.Sprintf("TRUNCATE TABLE %s CASCADE CONSTRAINTS", table)
		default:
			sql = fmt.Sprintf("TRUNCATE TABLE %s", table)
		}

		if err := t.db.Exec(sql).Error; err != nil {
			return fmt.Errorf("failed to truncate table %s: %w", table, err)
		}

		// Re-enable foreign key checks for MySQL
		if dialect == "mysql" {
			if err := t.db.Exec("SET FOREIGN_KEY_CHECKS = 1").Error; err != nil {
				return fmt.Errorf("failed to re-enable foreign key checks: %w", err)
			}
		}
	}
	return nil
}

// TruncateAll truncates all TMI tables in the correct dependency order
func (t *TestDB) TruncateAll() error {
	// Order matters - delete children before parents
	tables := []string{
		"session_participants",
		"collaboration_sessions",
		"webhook_deliveries",
		"webhook_subscriptions",
		"webhook_quotas",
		"webhook_url_deny_lists",
		"addon_invocation_quotas",
		"addons",
		"user_api_quotas",
		"administrators",
		"group_members",
		"threat_model_access",
		"metadata",
		"repositories",
		"notes",
		"documents",
		"threats",
		"assets",
		"diagrams",
		"threat_models",
		"client_credentials",
		"refresh_token_records",
		"groups",
		"users",
	}
	return t.Truncate(tables...)
}

// Count returns the count of records in a table
func (t *TestDB) Count(tableName string) (int64, error) {
	var count int64
	if err := t.db.Table(tableName).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// RawQuery executes a raw SQL query and returns results
func (t *TestDB) RawQuery(sql string, dest interface{}, args ...interface{}) error {
	return t.db.Raw(sql, args...).Scan(dest).Error
}

// Exec executes a raw SQL statement
func (t *TestDB) Exec(sql string, args ...interface{}) error {
	return t.db.Exec(sql, args...).Error
}

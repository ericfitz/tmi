// Package main implements diagnose-oracle, a diagnostic tool for investigating
// Oracle GORM issues, specifically the ORA-01400 error when inserting threats.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func main() {
	// Command line flags
	var (
		configFile = flag.String("config", "", "Path to TMI configuration file (required)")
		verbose    = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	// Validate required flags
	if *configFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --config flag is required")
		fmt.Fprintln(os.Stderr, "Usage: diagnose-oracle --config=<config-file>")
		os.Exit(1)
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

	log.Info("Oracle Diagnostic Tool")
	log.Info("  Config: %s", *configFile)

	// Create database connection using testdb package
	log.Info("Connecting to database...")
	db, err := testdb.New(*configFile)
	if err != nil {
		log.Error("Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Error closing database: %v", closeErr)
		}
	}()

	log.Info("Connected to %s database", db.DialectName())
	gormDB := db.DB()

	fmt.Println()
	fmt.Println("=== THREATS TABLE COLUMNS ===")
	checkTableColumns(gormDB, "THREATS")

	fmt.Println()
	fmt.Println("=== TRIGGERS ON THREATS TABLE ===")
	checkTriggers(gormDB, "THREATS")

	fmt.Println()
	fmt.Println("=== THREAT_MODELS TABLE COLUMNS (for comparison) ===")
	checkTableColumns(gormDB, "THREAT_MODELS")

	fmt.Println()
	fmt.Println("=== TEST 1: Raw SQL INSERT into THREATS ===")
	testRawSQLInsert(gormDB)

	fmt.Println()
	fmt.Println("=== TEST 2: GORM Insert with Minimal Struct (no auto-timestamp tags) ===")
	testGormMinimalInsert(gormDB)

	fmt.Println()
	fmt.Println("=== TEST 3: GORM Insert with Full models.Threat Struct ===")
	testGormFullInsert(gormDB)

	fmt.Println()
	fmt.Println("=== TEST 4: Check GORM Schema FieldsWithDefaultDBValue ===")
	checkGormSchema(gormDB)

	fmt.Println()
	log.Info("Diagnostic complete")
}

func checkTableColumns(db *gorm.DB, tableName string) {
	var columns []struct {
		ColumnName  string  `gorm:"column:COLUMN_NAME"`
		DataType    string  `gorm:"column:DATA_TYPE"`
		Nullable    string  `gorm:"column:NULLABLE"`
		DataDefault *string `gorm:"column:DATA_DEFAULT"`
	}
	result := db.Raw("SELECT column_name, data_type, nullable, data_default FROM user_tab_columns WHERE table_name = ? ORDER BY column_id", tableName).Scan(&columns)
	if result.Error != nil {
		fmt.Printf("  ERROR: Failed to get columns: %v\n", result.Error)
		return
	}
	if len(columns) == 0 {
		fmt.Printf("  Table %s not found or has no columns\n", tableName)
		return
	}
	for _, col := range columns {
		defaultVal := "none"
		if col.DataDefault != nil && *col.DataDefault != "" {
			defaultVal = *col.DataDefault
		}
		fmt.Printf("  %-25s %-20s nullable=%-3s default=%s\n", col.ColumnName, col.DataType, col.Nullable, defaultVal)
	}
}

func checkTriggers(db *gorm.DB, tableName string) {
	var triggers []struct {
		TriggerName string `gorm:"column:TRIGGER_NAME"`
		TriggerType string `gorm:"column:TRIGGER_TYPE"`
		Status      string `gorm:"column:STATUS"`
	}
	result := db.Raw("SELECT trigger_name, trigger_type, status FROM user_triggers WHERE table_name = ?", tableName).Scan(&triggers)
	if result.Error != nil {
		fmt.Printf("  ERROR: Failed to get triggers: %v\n", result.Error)
		return
	}
	if len(triggers) == 0 {
		fmt.Println("  No triggers found")
		return
	}
	for _, t := range triggers {
		fmt.Printf("  %s: %s (status: %s)\n", t.TriggerName, t.TriggerType, t.Status)
	}
}

func testRawSQLInsert(db *gorm.DB) {
	// First, we need a valid threat_model_id. Let's find one or create a dummy.
	var tmID string
	result := db.Raw("SELECT id FROM threat_models WHERE ROWNUM = 1").Scan(&tmID)
	if result.Error != nil || tmID == "" {
		fmt.Println("  No threat models found, skipping raw SQL test")
		return
	}

	testID := uuid.New().String()
	now := time.Now().UTC()

	sql := `INSERT INTO threats (ID, THREAT_MODEL_ID, NAME, THREAT_TYPE, MITIGATED, CREATED_AT, MODIFIED_AT)
		VALUES (:1, :2, :3, :4, :5, :6, :7)`

	fmt.Printf("  Using ThreatModel: %s\n", tmID)
	fmt.Printf("  Test ID: %s\n", testID)
	fmt.Printf("  SQL: %s\n", sql)

	result = db.Exec(sql, testID, tmID, "Raw SQL Test Threat", `["Spoofing"]`, 0, now, now)
	if result.Error != nil {
		fmt.Printf("  FAILED: %v\n", result.Error)
	} else {
		fmt.Printf("  SUCCESS! Rows affected: %d\n", result.RowsAffected)
		// Clean up
		db.Exec("DELETE FROM threats WHERE id = ?", testID)
		fmt.Println("  (Test row deleted)")
	}
}

func testGormMinimalInsert(db *gorm.DB) {
	// Minimal struct with NO auto-timestamp tags
	type MinimalThreat struct {
		ID            string    `gorm:"primaryKey;column:ID"`
		ThreatModelID string    `gorm:"column:THREAT_MODEL_ID"`
		Name          string    `gorm:"column:NAME"`
		ThreatType    string    `gorm:"column:THREAT_TYPE"`
		Mitigated     int       `gorm:"column:MITIGATED"`
		CreatedAt     time.Time `gorm:"column:CREATED_AT"`
		ModifiedAt    time.Time `gorm:"column:MODIFIED_AT"`
	}

	// Find a threat model
	var tmID string
	result := db.Raw("SELECT id FROM threat_models WHERE ROWNUM = 1").Scan(&tmID)
	if result.Error != nil || tmID == "" {
		fmt.Println("  No threat models found, skipping test")
		return
	}

	testID := uuid.New().String()
	now := time.Now().UTC()

	minThreat := MinimalThreat{
		ID:            testID,
		ThreatModelID: tmID,
		Name:          "Minimal GORM Test Threat",
		ThreatType:    `["Tampering"]`,
		Mitigated:     0,
		CreatedAt:     now,
		ModifiedAt:    now,
	}

	fmt.Printf("  Creating with GORM (minimal struct): ID=%s\n", minThreat.ID)

	result = db.Table("threats").Create(&minThreat)
	if result.Error != nil {
		fmt.Printf("  FAILED: %v\n", result.Error)
	} else {
		fmt.Printf("  SUCCESS! Rows affected: %d\n", result.RowsAffected)
		// Clean up
		db.Exec("DELETE FROM threats WHERE id = ?", testID)
		fmt.Println("  (Test row deleted)")
	}
}

func testGormFullInsert(db *gorm.DB) {
	// Find a threat model
	var tmID string
	result := db.Raw("SELECT id FROM threat_models WHERE ROWNUM = 1").Scan(&tmID)
	if result.Error != nil || tmID == "" {
		fmt.Println("  No threat models found, skipping test")
		return
	}

	testID := uuid.New().String()
	now := time.Now().UTC()

	threat := models.Threat{
		ID:            testID,
		ThreatModelID: tmID,
		Name:          "Full GORM Test Threat",
		ThreatType:    models.StringArray{"Information Disclosure"},
		Mitigated:     models.OracleBool(false),
		CreatedAt:     now,
		ModifiedAt:    now,
	}

	fmt.Printf("  Creating with GORM (models.Threat): ID=%s\n", threat.ID)
	fmt.Printf("  Struct CreatedAt tag: %+v\n", threat.CreatedAt)

	result = db.Create(&threat)
	if result.Error != nil {
		fmt.Printf("  FAILED: %v\n", result.Error)
	} else {
		fmt.Printf("  SUCCESS! Rows affected: %d\n", result.RowsAffected)
		// Clean up
		db.Exec("DELETE FROM threats WHERE id = ?", testID)
		fmt.Println("  (Test row deleted)")
	}
}

func checkGormSchema(db *gorm.DB) {
	// Parse the schema for models.Threat
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&models.Threat{}); err != nil {
		fmt.Printf("  ERROR parsing schema: %v\n", err)
		return
	}

	schema := stmt.Schema
	fmt.Printf("  Table Name: %s\n", schema.Table)
	fmt.Printf("  Primary Fields: ")
	for _, f := range schema.PrimaryFields {
		fmt.Printf("%s ", f.DBName)
	}
	fmt.Println()

	fmt.Printf("  FieldsWithDefaultDBValue (%d fields):\n", len(schema.FieldsWithDefaultDBValue))
	for _, f := range schema.FieldsWithDefaultDBValue {
		fmt.Printf("    - %s (DBName: %s, HasDefaultValue: %v, AutoCreateTime: %v, AutoUpdateTime: %v)\n",
			f.Name, f.DBName, f.HasDefaultValue, f.AutoCreateTime, f.AutoUpdateTime)
	}

	if len(schema.FieldsWithDefaultDBValue) > 0 {
		fmt.Println()
		fmt.Println("  WARNING: FieldsWithDefaultDBValue > 0 causes Oracle driver to use RETURNING INTO")
		fmt.Println("           which corrupts bind variable indices and causes ORA-01400 errors.")
	}
}

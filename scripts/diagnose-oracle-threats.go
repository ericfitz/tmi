//go:build ignore

// Diagnose Oracle threats table issue

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/oracle-samples/gorm-oracle/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	password := os.Getenv("ORACLE_PASSWORD")
	if password == "" {
		fmt.Println("ERROR: ORACLE_PASSWORD environment variable not set")
		os.Exit(1)
	}

	walletLocation := os.Getenv("TNS_ADMIN")
	if walletLocation == "" {
		walletLocation = "/Users/efitz/Projects/tmi/wallet"
	}

	// Connect to Oracle
	dsn := fmt.Sprintf(`user=ADMIN password="%s" connectString=tmidb_medium configDir="%s"`, password, walletLocation)
	db, err := gorm.Open(oracle.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		fmt.Printf("Failed to connect to Oracle: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Connected to Oracle ADB")
	fmt.Println()

	// Check threats table structure
	fmt.Println("=== THREATS TABLE COLUMNS ===")
	var columns []struct {
		ColumnName   string  `gorm:"column:COLUMN_NAME"`
		DataType     string  `gorm:"column:DATA_TYPE"`
		Nullable     string  `gorm:"column:NULLABLE"`
		DataDefault  *string `gorm:"column:DATA_DEFAULT"`
	}
	result := db.Raw("SELECT column_name, data_type, nullable, data_default FROM user_tab_columns WHERE table_name = 'THREATS' ORDER BY column_id").Scan(&columns)
	if result.Error != nil {
		fmt.Printf("Failed to get columns: %v\n", result.Error)
	} else {
		for _, col := range columns {
			defaultVal := "none"
			if col.DataDefault != nil {
				defaultVal = *col.DataDefault
			}
			fmt.Printf("  %s: %s (nullable: %s, default: %s)\n", col.ColumnName, col.DataType, col.Nullable, defaultVal)
		}
	}
	fmt.Println()

	// Check for triggers on threats table
	fmt.Println("=== TRIGGERS ON THREATS TABLE ===")
	var triggers []struct {
		TriggerName string `gorm:"column:TRIGGER_NAME"`
		TriggerType string `gorm:"column:TRIGGER_TYPE"`
		Status      string `gorm:"column:STATUS"`
	}
	result = db.Raw("SELECT trigger_name, trigger_type, status FROM user_triggers WHERE table_name = 'THREATS'").Scan(&triggers)
	if result.Error != nil {
		fmt.Printf("Failed to get triggers: %v\n", result.Error)
	} else if len(triggers) == 0 {
		fmt.Println("  No triggers found")
	} else {
		for _, t := range triggers {
			fmt.Printf("  %s: %s (status: %s)\n", t.TriggerName, t.TriggerType, t.Status)
		}
	}
	fmt.Println()

	// Check for sequences
	fmt.Println("=== SEQUENCES (checking for ID-related) ===")
	var sequences []struct {
		SequenceName string `gorm:"column:SEQUENCE_NAME"`
	}
	result = db.Raw("SELECT sequence_name FROM user_sequences WHERE sequence_name LIKE '%THREAT%' OR sequence_name LIKE '%ID%'").Scan(&sequences)
	if result.Error != nil {
		fmt.Printf("Failed to get sequences: %v\n", result.Error)
	} else if len(sequences) == 0 {
		fmt.Println("  No relevant sequences found")
	} else {
		for _, s := range sequences {
			fmt.Printf("  %s\n", s.SequenceName)
		}
	}
	fmt.Println()

	// Test raw SQL insert
	fmt.Println("=== TESTING RAW SQL INSERT ===")
	testID := uuid.New().String()
	testTMID := "00000000-0000-0000-0000-000000000001" // dummy
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	sql := fmt.Sprintf(`INSERT INTO threats (ID, THREAT_MODEL_ID, NAME, THREAT_TYPE, MITIGATED, CREATED_AT, MODIFIED_AT)
		VALUES ('%s', '%s', 'Test Threat', '["Spoofing"]', 0, TO_TIMESTAMP('%s', 'YYYY-MM-DD HH24:MI:SS'), TO_TIMESTAMP('%s', 'YYYY-MM-DD HH24:MI:SS'))`,
		testID, testTMID, now, now)

	fmt.Printf("Executing: %s\n\n", sql)

	result = db.Exec(sql)
	if result.Error != nil {
		fmt.Printf("Raw SQL INSERT failed: %v\n", result.Error)
	} else {
		fmt.Printf("Raw SQL INSERT succeeded! Rows affected: %d\n", result.RowsAffected)
		// Clean up using parameterized query to prevent SQL injection
		db.Exec("DELETE FROM threats WHERE id = ?", testID)
		fmt.Println("Test row deleted")
	}
	fmt.Println()

	// Test GORM insert with minimal struct
	fmt.Println("=== TESTING GORM INSERT (minimal struct) ===")
	type MinimalThreat struct {
		ID            string    `gorm:"primaryKey;column:ID"`
		ThreatModelID string    `gorm:"column:THREAT_MODEL_ID"`
		Name          string    `gorm:"column:NAME"`
		ThreatType    string    `gorm:"column:THREAT_TYPE"`
		Mitigated     int       `gorm:"column:MITIGATED"`
		CreatedAt     time.Time `gorm:"column:CREATED_AT"`
		ModifiedAt    time.Time `gorm:"column:MODIFIED_AT"`
	}

	testID2 := uuid.New().String()
	minThreat := MinimalThreat{
		ID:            testID2,
		ThreatModelID: testTMID,
		Name:          "GORM Test Threat",
		ThreatType:    `["Tampering"]`,
		Mitigated:     0,
		CreatedAt:     time.Now().UTC(),
		ModifiedAt:    time.Now().UTC(),
	}

	fmt.Printf("Creating with GORM: ID=%s, Name=%s\n", minThreat.ID, minThreat.Name)

	result = db.Table("threats").Create(&minThreat)
	if result.Error != nil {
		fmt.Printf("GORM INSERT failed: %v\n", result.Error)
	} else {
		fmt.Printf("GORM INSERT succeeded! Rows affected: %d\n", result.RowsAffected)
		// Clean up using parameterized query to prevent SQL injection
		db.Exec("DELETE FROM threats WHERE id = ?", testID2)
		fmt.Println("Test row deleted")
	}
}

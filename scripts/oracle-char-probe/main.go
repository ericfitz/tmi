//go:build oracle

// oracle-char-probe is a one-shot diagnostic for the #379 design question:
// "Does GORM's AutoMigrate detect that VARCHAR2(N) (BYTE) differs from
// VARCHAR2(N CHAR) and issue an ALTER ... MODIFY?"
//
// Procedure:
//  1. Connect to the Oracle ADB pointed to by ORACLE_PASSWORD / TNS_ADMIN
//     / ORACLE_CONNECT_STRING environment variables (sourced from oci-env.sh).
//  2. Drop the probe table if it exists from a prior run.
//  3. AutoMigrate(StagedV1) — creates PROBE_CHAR_SEMANTICS with a Name string
//     declared as `gorm:"type:varchar(256);not null"`. On Oracle this lands
//     as VARCHAR2(256 BYTE) (Oracle default semantics).
//  4. Snapshot USER_TAB_COLUMNS for the column.
//  5. AutoMigrate(StagedV2) — re-declares Name as DBVarchar with size:256.
//     If GORM sees a real type diff it should issue
//     ALTER TABLE PROBE_CHAR_SEMANTICS MODIFY (NAME VARCHAR2(256 CHAR)).
//  6. Snapshot USER_TAB_COLUMNS again.
//  7. Print before/after CHAR_USED and exit with code 0 if the result is
//     interpretable (regardless of which way it went) or non-zero on driver
//     error.
//
// Run via: source scripts/oci-env.sh && \
//	go run -tags oracle ./scripts/oracle-char-probe/...
//
// Cleanup: the probe table is dropped at the end. Set PROBE_KEEP=1 to leave
// it in place for manual inspection.
package main

import (
	"fmt"
	"os"

	models "github.com/ericfitz/tmi/api/models"
	tmidb "github.com/ericfitz/tmi/auth/db"

	// Oracle driver — same dialect used by TMI in production
	"github.com/oracle-samples/gorm-oracle/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// StagedV1 is the "before" model: plain `type:varchar(256)` GORM tag.
// On Oracle, this emits VARCHAR2(256) which defaults to BYTE semantics.
type StagedV1 struct {
	ID   string `gorm:"primaryKey;type:varchar(36)"`
	Name string `gorm:"type:varchar(256);not null"`
}

// TableName overrides the default table name so we can refer to it without
// worrying about gorm's pluralization heuristics across runs.
func (StagedV1) TableName() string { return "PROBE_CHAR_SEMANTICS" }

// StagedV2 is the "after" model: same table, but Name is now DBVarchar.
// On Oracle, DBVarchar emits VARCHAR2(256 CHAR).
type StagedV2 struct {
	ID   string            `gorm:"primaryKey;type:varchar(36)"`
	Name models.DBVarchar  `gorm:"size:256;not null"`
}

func (StagedV2) TableName() string { return "PROBE_CHAR_SEMANTICS" }

// columnState captures the relevant fields from USER_TAB_COLUMNS for the
// probe column. CHAR_USED is the discriminator we care about: 'B' = byte
// semantics, 'C' = char semantics.
type columnState struct {
	TableName  string `gorm:"column:TABLE_NAME"`
	ColumnName string `gorm:"column:COLUMN_NAME"`
	DataType   string `gorm:"column:DATA_TYPE"`
	DataLength int    `gorm:"column:DATA_LENGTH"`
	CharLength int    `gorm:"column:CHAR_LENGTH"`
	CharUsed   string `gorm:"column:CHAR_USED"`
}

func main() {
	exitCode := 0
	defer func() { os.Exit(exitCode) }()

	password := os.Getenv("ORACLE_PASSWORD")
	walletDir := os.Getenv("TNS_ADMIN")
	connectString := os.Getenv("ORACLE_CONNECT_STRING")
	if connectString == "" {
		connectString = "tmiadb_tp"
	}
	user := os.Getenv("ORACLE_USER")
	if user == "" {
		user = "ADMIN"
	}

	if password == "" || walletDir == "" {
		fmt.Fprintln(os.Stderr, "ERROR: ORACLE_PASSWORD and TNS_ADMIN must be set (source scripts/oci-env.sh).")
		exitCode = 2
		return
	}

	dsn := fmt.Sprintf(`user="%s" password="%s" connectString="%s" configDir="%s"`,
		user, password, connectString, walletDir)

	db, err := gorm.Open(oracle.New(oracle.Config{
		DataSourceName:       dsn,
		SkipQuoteIdentifiers: true,
	}), &gorm.Config{
		// Mirror TMI's production naming strategy so the probe sees the same
		// uppercase identifiers TMI itself would create. Without this the
		// driver case-folding on read disagrees with case-sensitive ALTER
		// emit on write, and AutoMigrate tries to ADD existing columns.
		NamingStrategy: &tmidb.OracleNamingStrategy{
			NamingStrategy: schema.NamingStrategy{IdentifierMaxLength: 30},
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: gorm.Open: %v\n", err)
		exitCode = 1
		return
	}

	// Always start clean. Ignore errors from DROP — the table may not exist.
	if err := db.Exec("DROP TABLE PROBE_CHAR_SEMANTICS").Error; err != nil {
		fmt.Fprintf(os.Stderr, "(note) DROP returned: %v\n", err)
	}

	fmt.Println("=== Step 1: AutoMigrate StagedV1 (plain type:varchar(256)) ===")
	if err := db.AutoMigrate(&StagedV1{}); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: AutoMigrate(StagedV1): %v\n", err)
		exitCode = 1
		return
	}

	beforeState, err := snapshotColumn(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: snapshot before: %v\n", err)
		exitCode = 1
		return
	}
	printState("before AutoMigrate(StagedV2)", beforeState)

	fmt.Println("\n=== Step 2: AutoMigrate StagedV2 (DBVarchar size:256) ===")
	if err := db.AutoMigrate(&StagedV2{}); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: AutoMigrate(StagedV2): %v\n", err)
		exitCode = 1
		return
	}

	afterState, err := snapshotColumn(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: snapshot after: %v\n", err)
		exitCode = 1
		return
	}
	printState("after AutoMigrate(StagedV2)", afterState)

	fmt.Println("\n=== Probe verdict ===")
	switch {
	case beforeState.CharUsed == "B" && afterState.CharUsed == "C":
		fmt.Println("RESULT: PASS — GORM detected the BYTE→CHAR diff and issued ALTER.")
		fmt.Println("ACTION: proceed with #379 Batches 1–5 as designed. No sidecar SQL needed.")
	case beforeState.CharUsed == "B" && afterState.CharUsed == "B":
		fmt.Println("RESULT: GORM did NOT detect the diff — column is still BYTE-mode.")
		fmt.Println("ACTION: author scripts/oracle-migrate-varchar-char.sql per #379 Task 0.4.")
		fmt.Println("        Run after each batch deploy until empty result from verify-oracle-char-semantics.sql.")
	default:
		fmt.Printf("RESULT: UNEXPECTED — before=%s, after=%s. Investigate manually.\n",
			beforeState.CharUsed, afterState.CharUsed)
		exitCode = 1
	}

	if os.Getenv("PROBE_KEEP") == "" {
		if err := db.Exec("DROP TABLE PROBE_CHAR_SEMANTICS").Error; err != nil {
			fmt.Fprintf(os.Stderr, "(note) final DROP returned: %v\n", err)
		}
	} else {
		fmt.Println("\n(PROBE_KEEP set — probe table left in place at PROBE_CHAR_SEMANTICS)")
	}
}

func snapshotColumn(db *gorm.DB) (columnState, error) {
	var state columnState
	err := db.Raw(`
		SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE, DATA_LENGTH, CHAR_LENGTH, CHAR_USED
		FROM USER_TAB_COLUMNS
		WHERE TABLE_NAME = 'PROBE_CHAR_SEMANTICS' AND COLUMN_NAME = 'NAME'`).
		Scan(&state).Error
	return state, err
}

func printState(label string, s columnState) {
	fmt.Printf("  %s:\n", label)
	fmt.Printf("    TABLE_NAME=%s COLUMN_NAME=%s\n", s.TableName, s.ColumnName)
	fmt.Printf("    DATA_TYPE=%s  DATA_LENGTH=%d  CHAR_LENGTH=%d  CHAR_USED=%q\n",
		s.DataType, s.DataLength, s.CharLength, s.CharUsed)
}

//go:build oracle

// oracle-clob-like-probe is a one-shot diagnostic for issue #411:
// "Does `LOWER(clob_column) LIKE LOWER(?)` work on Oracle ADB?"
//
// Background:
//
//	#379 Batch 4 migrated ThreatModel.description and Threat.description from
//	VARCHAR2(2048) to CLOB on Oracle (NullableDBText -> CLOB). Two GORM list
//	filters still run `LOWER(description) LIKE LOWER(?)` on those columns:
//	  - api/database_store_gorm.go (applyThreatModelFilters)
//	  - api/threat_store_gorm.go   (GormThreatRepository.applyFilters)
//
//	Oracle's documentation says LIKE is not supported on CLOB columns in a
//	WHERE predicate. In practice, Oracle 19c ADB with MAX_STRING_SIZE=EXTENDED
//	performs an implicit CLOB->VARCHAR2 conversion and the query succeeds for
//	data <= 32767 bytes. TMI's API layer caps these fields at 2048 chars, so
//	this probe verifies the behavior empirically rather than trusting the doc.
//
// Procedure:
//
//  1. Connect to the Oracle ADB pointed to by ORACLE_PASSWORD / TNS_ADMIN /
//     ORACLE_CONNECT_STRING (sourced from scripts/oci-env.sh).
//  2. Drop the probe table if it exists from a prior run.
//  3. AutoMigrate(ClobProbe) — creates PROBE_CLOB_LIKE with a Description
//     column declared as models.NullableDBText, which lands as CLOB on Oracle.
//  4. Confirm the column is actually CLOB via USER_TAB_COLUMNS.
//  5. Insert three rows with distinct descriptions (including a 2048-char row
//     to exercise the API's maximum field length).
//  6. Run the exact filter the TMI repositories run:
//     `LOWER(description) LIKE LOWER(?)` with a '%substr%' pattern, and assert
//     the expected rows come back.
//  7. Exit 0 if the filter executes and returns the right rows; non-zero on
//     any driver error or wrong result.
//
// Run via:
//
//	source scripts/oci-env.sh && \
//	go run -tags oracle ./scripts/oracle-clob-like-probe/...
//
// Cleanup: the probe table is dropped at the end. Set PROBE_KEEP=1 to leave
// it in place for manual inspection.
package main

import (
	"fmt"
	"os"
	"strings"

	models "github.com/ericfitz/tmi/api/models"
	tmidb "github.com/ericfitz/tmi/auth/db"

	// Oracle driver — same dialect used by TMI in production.
	"github.com/oracle-samples/gorm-oracle/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// ClobProbe mirrors the relevant shape of ThreatModel/Threat: a UUID-ish key
// plus a Description declared as NullableDBText, which GormDBDataType maps to
// CLOB on Oracle (see api/models/types.go).
type ClobProbe struct {
	ID          string                `gorm:"primaryKey;type:varchar(36)"`
	Description models.NullableDBText `gorm:""`
}

func (ClobProbe) TableName() string { return "PROBE_CLOB_LIKE" }

// columnState captures the relevant USER_TAB_COLUMNS fields for the probe
// column. DATA_TYPE is the discriminator we care about: it must be "CLOB".
type columnState struct {
	ColumnName string `gorm:"column:COLUMN_NAME"`
	DataType   string `gorm:"column:DATA_TYPE"`
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

	// timezone=UTC keeps godror from emitting a warning when the local host TZ
	// differs from the database's SYSTIMESTAMP offset.
	dsn := fmt.Sprintf(`user="%s" password="%s" connectString="%s" configDir="%s" timezone=UTC`,
		user, password, connectString, walletDir)

	db, err := gorm.Open(oracle.New(oracle.Config{
		DataSourceName:       dsn,
		SkipQuoteIdentifiers: true,
	}), &gorm.Config{
		// Mirror TMI's production naming strategy so the probe sees the same
		// uppercase identifiers TMI itself creates.
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
	if err := db.Exec("DROP TABLE PROBE_CLOB_LIKE").Error; err != nil {
		fmt.Fprintf(os.Stderr, "(note) DROP returned: %v\n", err)
	}

	fmt.Println("=== Step 1: AutoMigrate ClobProbe (Description as NullableDBText) ===")
	if err := db.AutoMigrate(&ClobProbe{}); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: AutoMigrate(ClobProbe): %v\n", err)
		exitCode = 1
		return
	}

	state, err := snapshotColumn(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: snapshot column: %v\n", err)
		exitCode = 1
		return
	}
	fmt.Printf("  DESCRIPTION column DATA_TYPE=%q\n", state.DataType)
	if state.DataType != "CLOB" {
		fmt.Printf("RESULT: UNEXPECTED — DESCRIPTION is %q, not CLOB. "+
			"The premise of #411 does not hold on this database.\n", state.DataType)
		exitCode = 1
		return
	}

	fmt.Println("\n=== Step 2: Insert probe rows ===")
	// Row C carries a 2047-char description — at the API's 2048-char maximum
	// field length — so the LIKE probe exercises a value at the largest size
	// TMI will ever store in this column.
	bigDesc := strings.Repeat("x", 2040) + " NEEDLE"
	rows := []ClobProbe{
		{ID: "11111111-1111-1111-1111-111111111111",
			Description: models.NullableDBText{String: "The quick brown FOX jumps", Valid: true}},
		{ID: "22222222-2222-2222-2222-222222222222",
			Description: models.NullableDBText{String: "a lazy dog sleeps", Valid: true}},
		{ID: "33333333-3333-3333-3333-333333333333",
			Description: models.NullableDBText{String: bigDesc, Valid: true}},
	}
	if err := db.Create(&rows).Error; err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: insert probe rows: %v\n", err)
		exitCode = 1
		return
	}
	fmt.Printf("  inserted %d rows (largest description = %d chars)\n", len(rows), len(bigDesc))

	fmt.Println("\n=== Step 3: Run LOWER(description) LIKE LOWER(?) — the TMI filter ===")
	// This is byte-for-byte the predicate built by applyThreatModelFilters and
	// GormThreatRepository.applyFilters.
	ok := true

	// 3a. Case-insensitive substring match on a short row ("FOX" -> "fox").
	var caseHits []ClobProbe
	if err := db.Where("LOWER(description) LIKE LOWER(?)", "%fox%").Find(&caseHits).Error; err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: LIKE query (short row) failed: %v\n", err)
		exitCode = 1
		return
	}
	if len(caseHits) == 1 && caseHits[0].ID == rows[0].ID {
		fmt.Println("  PASS: LOWER(clob) LIKE LOWER('%fox%') matched the one expected row.")
	} else {
		ok = false
		fmt.Printf("  FAIL: expected exactly row 1 for '%%fox%%', got %d rows.\n", len(caseHits))
	}

	// 3b. Substring match that lands inside the 2048-char row.
	var bigHits []ClobProbe
	if err := db.Where("LOWER(description) LIKE LOWER(?)", "%needle%").Find(&bigHits).Error; err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: LIKE query (2048-char row) failed: %v\n", err)
		exitCode = 1
		return
	}
	if len(bigHits) == 1 && bigHits[0].ID == rows[2].ID {
		fmt.Println("  PASS: LOWER(clob) LIKE LOWER('%needle%') matched the 2048-char row.")
	} else {
		ok = false
		fmt.Printf("  FAIL: expected exactly row 3 for '%%needle%%', got %d rows.\n", len(bigHits))
	}

	// 3c. A pattern that should match nothing.
	var noHits []ClobProbe
	if err := db.Where("LOWER(description) LIKE LOWER(?)", "%nonexistent%").Find(&noHits).Error; err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: LIKE query (no-match) failed: %v\n", err)
		exitCode = 1
		return
	}
	if len(noHits) == 0 {
		fmt.Println("  PASS: LOWER(clob) LIKE LOWER('%nonexistent%') matched zero rows.")
	} else {
		ok = false
		fmt.Printf("  FAIL: expected zero rows for '%%nonexistent%%', got %d.\n", len(noHits))
	}

	fmt.Println("\n=== Probe verdict ===")
	if ok {
		fmt.Println("RESULT: PASS — LOWER(CLOB) LIKE LOWER(?) executes correctly on Oracle ADB.")
		fmt.Println("ACTION: no change needed to the ThreatModel/Threat description filters (#411).")
	} else {
		fmt.Println("RESULT: FAIL — the filter did not behave as expected on Oracle ADB.")
		fmt.Println("ACTION: apply one of the #411 resolution options (DBMS_LOB.SUBSTR wrapper,")
		fmt.Println("        Oracle Text index, or a VARCHAR2 shadow column).")
		exitCode = 1
	}

	if os.Getenv("PROBE_KEEP") == "" {
		if err := db.Exec("DROP TABLE PROBE_CLOB_LIKE").Error; err != nil {
			fmt.Fprintf(os.Stderr, "(note) final DROP returned: %v\n", err)
		}
	} else {
		fmt.Println("\n(PROBE_KEEP set — probe table left in place at PROBE_CLOB_LIKE)")
	}
}

func snapshotColumn(db *gorm.DB) (columnState, error) {
	var state columnState
	err := db.Raw(`
		SELECT COLUMN_NAME, DATA_TYPE
		FROM USER_TAB_COLUMNS
		WHERE TABLE_NAME = 'PROBE_CLOB_LIKE' AND COLUMN_NAME = 'DESCRIPTION'`).
		Scan(&state).Error
	return state, err
}

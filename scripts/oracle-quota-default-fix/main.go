//go:build oracle

// oracle-quota-default-fix is a one-shot repair for issue #682: TMI's
// additive Oracle migrator (auth/db/gorm_oracle.go) no-ops GORM's
// MigrateColumn on Oracle, so a struct-tag `default:` change never reaches an
// already-provisioned ADB column — only PostgreSQL self-heals on every
// AutoMigrate. #649 changed the GORM defaults for two quota columns
// (UserAPIQuota.MaxRequestsPerMinute: 100 -> 1000, AddonInvocationQuota.
// MaxActiveInvocations: 1 -> 3) but the Oracle columns kept their original
// DEFAULT clause, silently diverging from PostgreSQL and from new callers
// that rely on the struct tag.
//
// Procedure:
//
//  1. Connect to the Oracle ADB pointed to by ORACLE_PASSWORD / TNS_ADMIN /
//     ORACLE_CONNECT_STRING (sourced from scripts/oci-env.sh) — same
//     bootstrap as scripts/oracle-clob-like-probe.
//  2. Query USER_TAB_COLUMNS for the current DATA_DEFAULT (and DEFAULT_LENGTH,
//     a plain NUMBER column that stays readable even when the driver cannot
//     scan the LONG-typed DATA_DEFAULT) of both columns and print the before
//     state. Aborts if neither target column is found under ORACLE_USER's
//     schema — that means the connection points at the wrong schema, not that
//     the repair is unnecessary.
//  3. Run the two ALTER TABLE ... MODIFY (... DEFAULT ...) statements that
//     PostgreSQL would have applied for free. Each is metadata-only and
//     idempotent — re-running this program after a successful prior run is
//     safe and a no-op. The two statements are NOT atomic with each other
//     (Oracle implicitly commits around DDL, so there is no multi-statement
//     transaction to roll back); if the first succeeds and the second fails,
//     the first's effect persists and only the second needs re-running.
//  4. Re-query and print the after state.
//  5. Count rows still carrying the stale literal default value. This is
//     read-only — the issue treats a data backfill as optional because every
//     current write path passes an explicit value; nonzero counts are
//     reported to the orchestrator/issue, not UPDATEd by this script. Note
//     the count is a superset signal: it matches any row whose value equals
//     the old default, including rows where an operator or caller
//     deliberately set that same value on purpose, not only rows that
//     inherited it from the stale DEFAULT.
//
// Run via:
//
//	source scripts/oci-env.sh && \
//	go run -tags oracle ./scripts/oracle-quota-default-fix/...
package main

import (
	"fmt"
	"os"

	tmidb "github.com/ericfitz/tmi/auth/db"

	// Oracle driver — same dialect used by TMI in production.
	"github.com/oracle-samples/gorm-oracle/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// columnDefault captures the relevant USER_TAB_COLUMNS fields for one of the
// two quota columns under repair. DATA_DEFAULT is declared LONG in the Oracle
// data dictionary, and some driver/scan combinations cannot materialize a
// LONG into a Go string — callers must tolerate that scan failing without
// treating it as fatal. DEFAULT_LENGTH (a plain NUMBER — the byte length of
// the default expression, available since Oracle 12.2) has no such
// restriction and is queried separately so there is always some evidence the
// ALTER took effect even when DATA_DEFAULT itself is unreadable.
// SEM@7bbef7cb69972ca979324137a10e8f644c604934: model an Oracle USER_TAB_COLUMNS row's DATA_DEFAULT for a quota column
type columnDefault struct {
	TableName   string `gorm:"column:TABLE_NAME"`
	ColumnName  string `gorm:"column:COLUMN_NAME"`
	DataDefault string `gorm:"column:DATA_DEFAULT"`
}

// SEM@7bbef7cb69972ca979324137a10e8f644c604934: model an Oracle USER_TAB_COLUMNS row's DEFAULT_LENGTH for a quota column
type columnDefaultLength struct {
	TableName     string `gorm:"column:TABLE_NAME"`
	ColumnName    string `gorm:"column:COLUMN_NAME"`
	DefaultLength *int64 `gorm:"column:DEFAULT_LENGTH"`
}

const columnFilter = `
	(TABLE_NAME = 'USER_API_QUOTAS' AND COLUMN_NAME = 'MAX_REQUESTS_PER_MINUTE')
	OR (TABLE_NAME = 'ADDON_INVOCATION_QUOTAS' AND COLUMN_NAME = 'MAX_ACTIVE_INVOCATIONS')`

// SEM@7bbef7cb69972ca979324137a10e8f644c604934: repair stale Oracle DEFAULT clauses on two quota columns and report stale rows (mutates DB)
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
		// Mirror TMI's production naming strategy so the fixer sees the same
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

	fmt.Println("=== Step 1: BEFORE state ===")
	lengths, err := queryDefaultLengths(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: query DEFAULT_LENGTH: %v\n", err)
		exitCode = 1
		return
	}
	if len(lengths) == 0 {
		fmt.Fprintf(os.Stderr,
			"ERROR: neither USER_API_QUOTAS.MAX_REQUESTS_PER_MINUTE nor "+
				"ADDON_INVOCATION_QUOTAS.MAX_ACTIVE_INVOCATIONS was found under "+
				"schema %q. This means ORACLE_USER (or ORACLE_CONNECT_STRING) points "+
				"at a schema that does not own these tables, not that the repair is "+
				"unnecessary. Set ORACLE_USER to the schema owner and retry.\n", user)
		exitCode = 1
		return
	}
	printDefaults(db, "before", lengths)

	fmt.Println("\n=== Step 2: ALTER TABLE ... MODIFY DEFAULT ===")
	// Live DML against these tables can hold the lock ALTER needs; without a
	// bounded wait, MODIFY fails immediately with ORA-00054 instead of simply
	// waiting out a short-lived writer.
	if err := db.Exec("ALTER SESSION SET DDL_LOCK_TIMEOUT = 30").Error; err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: ALTER SESSION SET DDL_LOCK_TIMEOUT: %v (continuing without a DDL lock wait)\n", err)
	}
	alters := []string{
		"ALTER TABLE user_api_quotas MODIFY (max_requests_per_minute DEFAULT 1000)",
		"ALTER TABLE addon_invocation_quotas MODIFY (max_active_invocations DEFAULT 3)",
	}
	// Each ALTER is independent and idempotent; a failure on one should not
	// suppress the other, and either way we still want to see the after-state
	// and stale-row counts to know exactly what did and didn't take effect.
	for _, stmt := range alters {
		if err := db.Exec(stmt).Error; err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", stmt, err)
			exitCode = 1
			continue
		}
		fmt.Printf("  OK: %s\n", stmt)
	}

	fmt.Println("\n=== Step 3: AFTER state ===")
	afterLengths, err := queryDefaultLengths(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: query DEFAULT_LENGTH: %v\n", err)
		exitCode = 1
	} else {
		printDefaults(db, "after", afterLengths)
	}

	fmt.Println("\n=== Step 4: stale-row assessment (read-only, no UPDATE) ===")
	assessStaleRows(db, "user_api_quotas", "max_requests_per_minute", 100)
	assessStaleRows(db, "addon_invocation_quotas", "max_active_invocations", 1)

	if exitCode == 0 {
		fmt.Println("\nRESULT: quota column DEFAULTs repaired. Nonzero stale-row counts above are")
		fmt.Println("informational only — record them on issue #682; every current write path")
		fmt.Println("passes an explicit quota value, so an UPDATE backfill was judged optional.")
	} else {
		fmt.Println("\nRESULT: one or more steps reported an error above — review before closing #682.")
	}
}

// queryDefaultLengths queries DEFAULT_LENGTH (a plain NUMBER, always
// readable) for the two target columns and doubles as the existence check:
// an empty result means the connected schema does not own these tables
// (reads DB).
// SEM@7bbef7cb69972ca979324137a10e8f644c604934: fetch DEFAULT_LENGTH for the two target quota columns from Oracle (reads DB)
func queryDefaultLengths(db *gorm.DB) ([]columnDefaultLength, error) {
	var rows []columnDefaultLength
	err := db.Raw(`
		SELECT TABLE_NAME, COLUMN_NAME, DEFAULT_LENGTH FROM user_tab_columns
		WHERE ` + columnFilter).
		Scan(&rows).Error
	return rows, err
}

// printDefaults prints DATA_DEFAULT for the two quota columns under repair,
// tolerating a driver-level scan failure on the LONG column type, and always
// prints the caller-supplied DEFAULT_LENGTH rows regardless of whether
// DATA_DEFAULT was readable — so an unreadable/empty LONG still leaves
// numeric evidence (byte length of the default expression) that the ALTER
// took effect (reads DB).
// SEM@7bbef7cb69972ca979324137a10e8f644c604934: format and print the quota columns' current DEFAULT clause state (reads DB)
func printDefaults(db *gorm.DB, label string, lengths []columnDefaultLength) {
	var rows []columnDefault
	err := db.Raw(`
		SELECT TABLE_NAME, COLUMN_NAME, DATA_DEFAULT FROM user_tab_columns
		WHERE ` + columnFilter).
		Scan(&rows).Error
	if err != nil {
		// DATA_DEFAULT is LONG. Some driver/scan paths cannot materialize a
		// LONG via a plain SELECT ... Scan — it can still be read via a PL/SQL
		// block (implicit LONG->VARCHAR2 conversion) or DBMS_METADATA.GET_DDL,
		// but for a one-off repair script DEFAULT_LENGTH below is a simpler
		// cross-check that needs no such workaround.
		fmt.Printf("  [%s] DATA_DEFAULT unreadable via driver scan, proceeding: %v\n", label, err)
	} else {
		for _, r := range rows {
			fmt.Printf("  [%s] %s.%s DATA_DEFAULT=%q\n", label, r.TableName, r.ColumnName, r.DataDefault)
		}
	}
	for _, l := range lengths {
		length := "NULL"
		if l.DefaultLength != nil {
			length = fmt.Sprintf("%d", *l.DefaultLength)
		}
		fmt.Printf("  [%s] %s.%s DEFAULT_LENGTH=%s\n", label, l.TableName, l.ColumnName, length)
	}
}

// assessStaleRows counts rows carrying the pre-#649 literal default value for
// one quota column and prints the count (reads DB; no mutation). The count is
// a superset of "still using the old default": it also matches any row where
// that same value was deliberately set on purpose, not only rows that
// inherited it from the stale DEFAULT — read it as an upper bound, not proof
// of staleness.
// SEM@7bbef7cb69972ca979324137a10e8f644c604934: count rows still carrying a quota column's stale literal default value (reads DB)
func assessStaleRows(db *gorm.DB, table, column string, staleValue int) {
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ?", table, column)
	if err := db.Raw(query, staleValue).Scan(&count).Error; err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: stale-row count for %s.%s: %v\n", table, column, err)
		return
	}
	fmt.Printf("  %s.%s = %d (matches old default; may include deliberately-set values): %d row(s)\n", table, column, staleValue, count)
}

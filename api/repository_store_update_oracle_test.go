//go:build oracle

package api

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Oracle-gated coverage for the document and repository Update paths (#616).
//
// These paths shipped an ORA-01407 blocking bug precisely because nothing here
// exercised them on Oracle: Session{SkipHooks:true} silently removed the
// validation, the failure was Oracle-only, and #610's verification ("PUT
// .../repositories/bulk returns 200", "modified_at now advances") ran against
// the dev cluster — PostgreSQL. A green PG run is not evidence for these paths.
//
// Three properties, matching the issue's proposal:
//
//  1. exactly one MODIFIED_AT assignment is emitted (the ORA-00957 hazard in
//     #615 — a lowercase map key misses GORM's autoUpdateTime dedup on Oracle,
//     where field.DBName is MODIFIED_AT, and a second assignment to the same
//     column is appended),
//  2. modified_at actually advances past created_at on update,
//  3. emptying a NOT NULL string column is a clean rejection, not ORA-01407
//     surfacing as a 500.

func openStoreUpdateOracleDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbURL := os.Getenv("TMI_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TMI_DATABASE_URL not set; run under `make test-integration-oci` with scripts/oci-env.sh sourced")
	}

	cfg, err := authdb.ParseDatabaseURL(dbURL)
	require.NoError(t, err, "parse TMI_DATABASE_URL")
	require.Equal(t, authdb.DatabaseTypeOracle, cfg.Type,
		"this test requires an oracle:// TMI_DATABASE_URL (got %q)", cfg.Type)

	if cfg.OracleWalletLocation == "" {
		if w := os.Getenv("TMI_ORACLE_WALLET_LOCATION"); w != "" {
			cfg.OracleWalletLocation = w
		} else if w := os.Getenv("TNS_ADMIN"); w != "" {
			cfg.OracleWalletLocation = w
		}
	}

	gormDB, err := authdb.NewGormDB(*cfg)
	require.NoError(t, err, "open Oracle GORM connection")
	t.Cleanup(func() { _ = gormDB.Close() })
	return gormDB.DB()
}

// captureSQL records the statement GORM emits for the next operation on the
// returned session, without executing it. DryRun keeps this independent of
// table state, which is what lets the ORA-00957 assertion be about the SQL
// itself rather than about whether a particular row happens to exist.
func captureSQL(t *testing.T, db *gorm.DB, build func(tx *gorm.DB) *gorm.DB) string {
	t.Helper()
	stmt := build(db.Session(&gorm.Session{DryRun: true}))
	require.NotNil(t, stmt.Statement, "no statement produced")
	return stmt.Statement.SQL.String()
}

// The ORA-00957 guard. AssignmentMap uppercases the key on Oracle so it matches
// field.DBName, which both satisfies the autoUpdateTime dedup and hits
// FieldsByDBName directly — so MODIFIED_AT must appear exactly once in SET.
func TestRepositoryUpdateOracle_EmitsSingleModifiedAtAssignment(t *testing.T) {
	db := openStoreUpdateOracleDB(t)

	updates := map[string]any{
		"name":        "renamed",
		"modified_at": time.Now().UTC(),
	}
	sql := captureSQL(t, db, func(tx *gorm.DB) *gorm.DB {
		return tx.Session(&gorm.Session{SkipHooks: true}).Model(&models.Repository{}).
			Where("id = ?", uuid.New().String()).
			Updates(AssignmentMap(tx.Name(), updates))
	})

	upper := strings.ToUpper(sql)
	setClause := upper
	if i := strings.Index(upper, "SET"); i >= 0 {
		setClause = upper[i:]
		if j := strings.Index(setClause, "WHERE"); j >= 0 {
			setClause = setClause[:j]
		}
	}
	assert.Equal(t, 1, strings.Count(setClause, "MODIFIED_AT"),
		"MODIFIED_AT must be assigned exactly once; two assignments to the same "+
			"column is ORA-00957, and PostgreSQL never sees it. SET was: %s", setClause)
}

func TestDocumentUpdateOracle_EmitsSingleModifiedAtAssignment(t *testing.T) {
	db := openStoreUpdateOracleDB(t)

	updates := map[string]any{
		"name":        "renamed",
		"modified_at": time.Now().UTC(),
	}
	sql := captureSQL(t, db, func(tx *gorm.DB) *gorm.DB {
		return tx.Session(&gorm.Session{SkipHooks: true}).Model(&models.Document{}).
			Where("id = ?", uuid.New().String()).
			Updates(AssignmentMap(tx.Name(), updates))
	})

	upper := strings.ToUpper(sql)
	setClause := upper
	if i := strings.Index(upper, "SET"); i >= 0 {
		setClause = upper[i:]
		if j := strings.Index(setClause, "WHERE"); j >= 0 {
			setClause = setClause[:j]
		}
	}
	assert.Equal(t, 1, strings.Count(setClause, "MODIFIED_AT"),
		"MODIFIED_AT must be assigned exactly once (ORA-00957). SET was: %s", setClause)
}

// #610 made modified_at advance on the SkipHooks path by setting it explicitly.
// That was verified on PostgreSQL only; this is the Oracle half.
func TestRepositoryUpdateOracle_ModifiedAtAdvances(t *testing.T) {
	db := openStoreUpdateOracleDB(t)
	ctx := context.Background()

	tmID := uuid.New().String()
	rec := models.Repository{
		ID:            models.DBVarchar(uuid.New().String()),
		ThreatModelID: models.DBVarchar(tmID),
		Name:          models.NewNullableDBVarchar(strPtrOracle("initial")),
		URI:           models.DBText("https://example.com/r.git"),
		CreatedAt:     time.Now().UTC().Add(-time.Hour),
		ModifiedAt:    time.Now().UTC().Add(-time.Hour),
	}
	require.NoError(t, db.WithContext(ctx).Create(&rec).Error, "seed repository")
	t.Cleanup(func() {
		_ = db.WithContext(ctx).Where("id = ?", string(rec.ID)).Delete(&models.Repository{}).Error
	})

	before := rec.ModifiedAt
	updates := map[string]any{"name": "renamed", "modified_at": time.Now().UTC()}
	require.NoError(t, db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).
		Model(&models.Repository{}).
		Where("id = ?", string(rec.ID)).
		Updates(AssignmentMap(db.Name(), updates)).Error, "update repository")

	var after models.Repository
	require.NoError(t, db.WithContext(ctx).First(&after, "id = ?", string(rec.ID)).Error)
	assert.True(t, after.ModifiedAt.After(before),
		"modified_at must advance on update; SkipHooks disables autoUpdateTime, "+
			"so a frozen timestamp here means the explicit assignment was lost "+
			"(the #610 defect, in its Oracle form)")
}

// The ORA-01407 case in its raw form: Oracle binds ” as NULL, so writing an
// empty string to a NOT NULL column is a constraint violation, and it MUST
// classify as ErrConstraint (400) rather than escaping as an unclassified 500.
func TestRepositoryUpdateOracle_EmptyNotNullIsConstraintNot500(t *testing.T) {
	db := openStoreUpdateOracleDB(t)
	ctx := context.Background()

	tmID := uuid.New().String()
	rec := models.Repository{
		ID:            models.DBVarchar(uuid.New().String()),
		ThreatModelID: models.DBVarchar(tmID),
		Name:          models.NewNullableDBVarchar(strPtrOracle("initial")),
		URI:           models.DBText("https://example.com/r.git"),
		CreatedAt:     time.Now().UTC(),
		ModifiedAt:    time.Now().UTC(),
	}
	require.NoError(t, db.WithContext(ctx).Create(&rec).Error, "seed repository")
	t.Cleanup(func() {
		_ = db.WithContext(ctx).Where("id = ?", string(rec.ID)).Delete(&models.Repository{}).Error
	})

	err := db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).
		Model(&models.Repository{}).
		Where("id = ?", string(rec.ID)).
		Updates(AssignmentMap(db.Name(), map[string]any{"uri": ""})).Error

	require.Error(t, err, "Oracle binds '' as NULL; URI is NOT NULL, so this must fail")
	classified := dberrors.Classify(err)
	assert.ErrorIs(t, classified, dberrors.ErrConstraint,
		"ORA-01407 must classify as ErrConstraint so handlers answer 400, not 500 "+
			"(the Zero-500 violation this test exists to keep closed): %v", err)
}

func strPtrOracle(s string) *string { return &s }

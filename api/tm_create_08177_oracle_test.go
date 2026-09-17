//go:build oracle

package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestThreatModelCreateFalse08177OracleIntegration is the #903 regression:
// back-to-back threat model creates with no concurrent writer must not raise
// ORA-08177. Under SERIALIZABLE ~8% of creates hit a false one (an index leaf
// split commits recursively past the snapshot), so 60 clean creates leave
// under a 1% chance of passing with the bug present. The callback counts the
// attempts the retry wrapper would otherwise hide. tmiadb is shared: a real
// concurrent writer can raise a legitimate ORA-08177 here, so rule that out
// before blaming the isolation level.
//
// Run via `make test-integration-oci`.
// SEM@178dbd0418cfb7e057d4297c7a88c5879cb64c7f: verify back-to-back threat model creates raise no false ORA-08177 on Oracle ADB (writes DB)
func TestThreatModelCreateFalse08177OracleIntegration(t *testing.T) {
	// Bounded for the same #671 reason as the alias sequence tests.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db := openAuditAppendOnlyOracleDB(t).WithContext(ctx)
	if !db.Migrator().HasTable(&models.ThreatModelAccess{}) {
		t.Skip("application schema not present on this database; start the server against it once first")
	}

	hits := map[string]int{}
	require.NoError(t, db.Callback().Create().After("gorm:create").Register("count08177", func(tx *gorm.DB) {
		if tx.Error != nil && strings.Contains(tx.Error.Error(), "ORA-08177") {
			hits[tx.Statement.Table]++
		}
	}))

	require.NoError(t, dbschema.InstallThreatModelAliasSequence(ctx, db))
	prev := useAliasSequence.Load()
	EnableThreatModelAliasSequence()
	t.Cleanup(func() { useAliasSequence.Store(prev) })

	userID := uuid.New().String()
	pid := "repro903-" + userID
	require.NoError(t, db.Create(&models.User{
		InternalUUID:   models.DBVarchar(userID),
		Provider:       "tmi",
		ProviderUserID: models.NewNullableDBVarchar(&pid),
		Email:          models.DBVarchar(pid + "@tmi.local"),
		Name:           "repro903",
	}).Error)

	store := NewGormThreatModelStore(db)
	var ids []string
	t.Cleanup(func() {
		for _, id := range ids {
			_ = db.Exec("DELETE FROM THREAT_MODEL_ACCESS WHERE THREAT_MODEL_ID = ?", id).Error
			_ = db.Exec("DELETE FROM THREAT_MODELS WHERE ID = ?", id).Error
		}
		_ = db.Exec("DELETE FROM USERS WHERE INTERNAL_UUID = ?", userID).Error
	})

	owner := User{ProviderId: userID, Provider: "tmi"}
	const creates = 60
	failures := 0
	start := time.Now()
	for i := 0; i < creates; i++ {
		auth := []Authorization{{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: userID, Role: AuthorizationRoleOwner}}
		_, err := store.Create(ThreatModel{Name: "repro903", Owner: owner, CreatedBy: &owner, Authorization: &auth},
			func(tm ThreatModel, id string) ThreatModel { ids = append(ids, id); return tm })
		if err != nil {
			failures++
			t.Logf("create %d failed: %v", i, err)
		}
	}
	t.Logf("%d creates in %v; exhausted=%d; ORA-08177 by table=%v", creates, time.Since(start), failures, hits)
	require.Zero(t, failures, "creates exhausted the serializable retry")
	require.Empty(t, hits, "false ORA-08177 with no concurrent writer")
}

//go:build oracle

package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/require"
)

// TestThreatModelListUserFiltersOracleIntegration is the #909 regression: the
// owner and security_reviewer list filters join USERS under a table alias,
// and Oracle rejects `JOIN users AS alias` with ORA-02000. The query only has
// to parse and run; matching rows are not required.
//
// Run via `make test-integration-oci`.
// SEM@0000000000000000000000000000000000000000: verify threat model list owner and reviewer filters execute on Oracle ADB (reads DB)
func TestThreatModelListUserFiltersOracleIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	db := openAuditAppendOnlyOracleDB(t).WithContext(ctx)
	if !db.Migrator().HasTable(&models.ThreatModel{}) {
		t.Skip("application schema not present on this database; start the server against it once first")
	}

	who := "no-such-user-909"
	store := NewGormThreatModelStore(db)
	_, _, err := store.ListWithCounts(0, 10, nil, &ThreatModelFilters{
		Owner:            &who,
		SecurityReviewer: &ParsedFilter{Operator: FilterOpNone, Value: who},
	})
	require.NoError(t, err)
}

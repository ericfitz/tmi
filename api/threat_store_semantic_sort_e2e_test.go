package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestSemanticSortEndToEnd verifies that sort=severity/priority/status returns
// rows in canonical semantic order rather than lexicographic order (issue #280).
// Complements TestBuildOrderBy / TestBuildSemanticOrderExpr by exercising the full
// GORM pipeline: buildOrderBy -> query.Order -> SQLite execution.
// SEM@0240c1fcec8f4ca8131c426f999aba63828ded4e: verify threat list sorts by semantic rank, not lexicographic order (reads DB)
func TestSemanticSortEndToEnd(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Threat{}, &models.AliasCounter{}))

	store := &GormThreatRepository{db: db}
	tmUUID := uuid.New()
	tmID := tmUUID.String()
	ctx := context.Background()

	ptrStr := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}

	cases := []struct {
		field  string
		values []string
		set    func(*Threat, *string)
		get    func(Threat) string
	}{
		{
			field:  "severity",
			values: []string{"unknown", "informational", "low", "medium", "high", "critical"},
			set:    func(t *Threat, v *string) { t.Severity = v },
			get:    func(t Threat) string { return ptrStr(t.Severity) },
		},
		{
			field:  "priority",
			values: []string{"deferred", "low", "medium", "high", "immediate"},
			set:    func(t *Threat, v *string) { t.Priority = v },
			get:    func(t Threat) string { return ptrStr(t.Priority) },
		},
		{
			field:  "status",
			values: []string{"open", "confirmed", "deferred", "mitigation_in_progress", "resolved", "accepted", "false_positive"},
			set:    func(t *Threat, v *string) { t.Status = v },
			get:    func(t Threat) string { return ptrStr(t.Status) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.field+" canonical ascending order", func(t *testing.T) {
			require.NoError(t, db.Exec("DELETE FROM threats").Error)

			scrambled := append([]string(nil), tc.values...)
			scrambled[0], scrambled[len(scrambled)-1] = scrambled[len(scrambled)-1], scrambled[0]
			for _, v := range scrambled {
				tid := uuid.New()
				desc := "desc"
				val := v
				th := &Threat{
					Id:            &tid,
					ThreatModelId: &tmUUID,
					Name:          tc.field + "-" + v,
					Description:   &desc,
					ThreatType:    []string{"test"},
				}
				tc.set(th, &val)
				require.NoError(t, store.Create(ctx, th))
			}

			sort := tc.field + ":asc"
			filter := ThreatFilter{Sort: &sort, Offset: 0, Limit: 100}
			results, _, err := store.List(ctx, tmID, filter)
			require.NoError(t, err)
			require.Len(t, results, len(tc.values))

			got := make([]string, len(results))
			for i, r := range results {
				got[i] = tc.get(r)
			}
			assert.Equal(t, tc.values, got, "%s sorted asc should match canonical order", tc.field)
		})
	}
}

// TestSortPaginationStability is an intent-documenting smoke test, not a
// proven regression guard: SQLite orders rows tied on the sort key by rowid
// deterministically, so this passes whether or not buildOrderBy emits an id
// tiebreaker. TestSortPaginationStability_Integration
// (api/threat_sort_pagination_integration_test.go) runs the same scenario
// end-to-end against PostgreSQL, but empirically (mutation-tested by hand:
// temporarily dropping the ", id ASC" suffix and rerunning it) PostgreSQL's
// tuplesort was also stable enough on this fresh, sequentially-inserted data
// that it passed without the tiebreaker too -- so neither pagination test
// should be relied on to catch a regression here. The property that IS
// proven to catch a reverted tiebreaker is the exact ORDER BY / CASE WHEN
// text pinned in TestBuildOrderBy and TestBuildSemanticOrderExpr
// (api/threat_store_gorm_test.go); those are the real regression guards.
// SEM@0240c1fcec8f4ca8131c426f999aba63828ded4e: verify LIMIT/OFFSET pagination never drops or duplicates rows tied on the sort key (reads DB)
func TestSortPaginationStability(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Threat{}, &models.AliasCounter{}))

	store := &GormThreatRepository{db: db}
	tmUUID := uuid.New()
	tmID := tmUUID.String()
	ctx := context.Background()

	const total = 25
	allIDs := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		tid := uuid.New()
		desc := "desc"
		severity := "high"
		th := &Threat{
			Id:            &tid,
			ThreatModelId: &tmUUID,
			Name:          fmt.Sprintf("threat-%d", i),
			Description:   &desc,
			ThreatType:    []string{"test"},
			Severity:      &severity,
		}
		require.NoError(t, store.Create(ctx, th))
		allIDs[tid.String()] = true
	}

	sort := "severity:asc"
	seen := make(map[string]bool, total)
	var duplicates []string
	for offset := 0; offset < total; offset += 7 {
		filter := ThreatFilter{Sort: &sort, Offset: offset, Limit: 7}
		results, _, err := store.List(ctx, tmID, filter)
		require.NoError(t, err)
		for _, r := range results {
			id := r.Id.String()
			if seen[id] {
				duplicates = append(duplicates, id)
			}
			seen[id] = true
		}
	}

	assert.Empty(t, duplicates, "no threat ID should appear across more than one page")
	assert.Equal(t, allIDs, seen, "union of all pages should equal the full inserted set")
}

// TestSeverityDescAcrossPages pins the order the UI expects from
// sort=severity:desc (#910): critical > high > medium > low > informational >
// unknown, with legacy stored values ranked as the client renders them and
// unranked or missing values last, read through LIMIT/OFFSET pages.
// SEM@c91b16ea67b50cc273cb925b803aeb2cac07d517: verify severity descending sort ranks current and legacy values across pages (reads DB)
func TestSeverityDescAcrossPages(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Threat{}, &models.AliasCounter{}))

	store := &GormThreatRepository{db: db}
	tmUUID := uuid.New()
	ctx := context.Background()

	// stored value -> label the client shows for it
	shown := map[string]string{
		"critical": "critical", "0": "critical", "Critical": "critical",
		"high": "high", "1": "high",
		"medium": "medium", "2": "medium",
		"low": "low", "3": "low",
		"informational": "informational", "4": "informational", "Info": "informational",
		"unknown": "unknown", "5": "unknown", "None": "unknown",
		"bogus": "other",
	}
	for stored := range shown {
		tid := uuid.New()
		desc := "desc"
		sev := stored
		require.NoError(t, store.Create(ctx, &Threat{
			Id: &tid, ThreatModelId: &tmUUID, Name: "t-" + stored,
			Description: &desc, ThreatType: []string{"test"}, Severity: &sev,
		}))
	}

	sort := "severity:desc"
	var got []string
	for offset := 0; offset < len(shown); offset += 5 {
		results, _, err := store.List(ctx, tmUUID.String(), ThreatFilter{Sort: &sort, Offset: offset, Limit: 5})
		require.NoError(t, err)
		for _, r := range results {
			got = append(got, shown[*r.Severity])
		}
	}

	want := []string{
		"critical", "critical", "critical", "high", "high", "medium", "medium", "low", "low",
		"informational", "informational", "informational", "unknown", "unknown", "unknown", "other",
	}
	assert.Equal(t, want, got)
}

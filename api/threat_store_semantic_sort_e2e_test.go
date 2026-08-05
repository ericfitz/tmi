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

// SEM@78155d54: verify LIMIT/OFFSET pagination never drops or duplicates rows tied on the sort key (reads DB)
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

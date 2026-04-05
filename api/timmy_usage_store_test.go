package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimmyUsageStore_RecordAndGet(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyUsageStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	usage := &models.TimmyUsage{
		UserID:           "user-001",
		SessionID:        "session-001",
		ThreatModelID:    "tm-usage-001",
		MessageCount:     5,
		PromptTokens:     100,
		CompletionTokens: 200,
		EmbeddingTokens:  50,
		PeriodStart:      now,
		PeriodEnd:        now.Add(time.Hour),
	}

	err := store.Record(ctx, usage)
	require.NoError(t, err)
	assert.NotEmpty(t, usage.ID)

	// Retrieve by user
	results, err := store.GetByUser(ctx, "user-001", now.Add(-time.Minute), now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "user-001", results[0].UserID)
	assert.Equal(t, "tm-usage-001", results[0].ThreatModelID)
	assert.Equal(t, 5, results[0].MessageCount)
	assert.Equal(t, 100, results[0].PromptTokens)
	assert.Equal(t, 200, results[0].CompletionTokens)
	assert.Equal(t, 50, results[0].EmbeddingTokens)

	// Out-of-range query returns empty
	empty, err := store.GetByUser(ctx, "user-001", now.Add(2*time.Hour), now.Add(3*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, empty)

	// Retrieve by threat model
	tmResults, err := store.GetByThreatModel(ctx, "tm-usage-001", now.Add(-time.Minute), now.Add(2*time.Hour))
	require.NoError(t, err)
	require.Len(t, tmResults, 1)
	assert.Equal(t, usage.ID, tmResults[0].ID)
}

func TestTimmyUsageStore_GetAggregated(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyUsageStore(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	tmID := "tm-usage-002"
	userID := "user-002"

	records := []*models.TimmyUsage{
		{
			UserID:           userID,
			SessionID:        "session-A",
			ThreatModelID:    tmID,
			MessageCount:     3,
			PromptTokens:     100,
			CompletionTokens: 150,
			EmbeddingTokens:  30,
			PeriodStart:      now,
			PeriodEnd:        now.Add(time.Hour),
		},
		{
			UserID:           userID,
			SessionID:        "session-B",
			ThreatModelID:    tmID,
			MessageCount:     5,
			PromptTokens:     200,
			CompletionTokens: 250,
			EmbeddingTokens:  40,
			PeriodStart:      now.Add(time.Hour),
			PeriodEnd:        now.Add(2 * time.Hour),
		},
		{
			UserID:           "other-user",
			SessionID:        "session-C",
			ThreatModelID:    tmID,
			MessageCount:     2,
			PromptTokens:     50,
			CompletionTokens: 75,
			EmbeddingTokens:  10,
			PeriodStart:      now,
			PeriodEnd:        now.Add(time.Hour),
		},
	}

	for _, r := range records {
		err := store.Record(ctx, r)
		require.NoError(t, err)
	}

	rangeStart := now.Add(-time.Minute)
	rangeEnd := now.Add(3 * time.Hour)

	// Aggregate by user and TM
	agg, err := store.GetAggregated(ctx, userID, tmID, rangeStart, rangeEnd)
	require.NoError(t, err)
	require.NotNil(t, agg)
	assert.Equal(t, 8, agg.TotalMessages)           // 3 + 5
	assert.Equal(t, 300, agg.TotalPromptTokens)     // 100 + 200
	assert.Equal(t, 400, agg.TotalCompletionTokens) // 150 + 250
	assert.Equal(t, 70, agg.TotalEmbeddingTokens)   // 30 + 40
	assert.Equal(t, 2, agg.SessionCount)            // session-A + session-B

	// Aggregate by TM only (all users)
	aggTM, err := store.GetAggregated(ctx, "", tmID, rangeStart, rangeEnd)
	require.NoError(t, err)
	require.NotNil(t, aggTM)
	assert.Equal(t, 10, aggTM.TotalMessages)          // 3 + 5 + 2
	assert.Equal(t, 350, aggTM.TotalPromptTokens)     // 100 + 200 + 50
	assert.Equal(t, 475, aggTM.TotalCompletionTokens) // 150 + 250 + 75
	assert.Equal(t, 80, aggTM.TotalEmbeddingTokens)   // 30 + 40 + 10
	assert.Equal(t, 3, aggTM.SessionCount)            // all three sessions

	// No matching records returns zeroes
	empty, err := store.GetAggregated(ctx, userID, tmID, now.Add(10*time.Hour), now.Add(11*time.Hour))
	require.NoError(t, err)
	require.NotNil(t, empty)
	assert.Equal(t, 0, empty.TotalMessages)
	assert.Equal(t, 0, empty.SessionCount)
}

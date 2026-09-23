package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SEM@0000000000000000000000000000000000000000: validate threat create and bulk create persist request metadata
func TestGormThreatRepository_CreatePersistsMetadata(t *testing.T) {
	db, tm := setupThreatAliasTestDB(t)
	repo := NewGormThreatRepository(db, nil, nil)
	ctx := context.Background()

	tmUUID, err := uuid.Parse(string(tm.ID))
	require.NoError(t, err)

	newThreat := func(name, key, value string) Threat {
		md := []Metadata{{Key: key, Value: value}}
		return Threat{Name: name, ThreatType: []string{"spoofing"}, ThreatModelId: &tmUUID, Metadata: &md}
	}
	assertMetadata := func(id *uuid.UUID, key, value string) {
		t.Helper()
		stored, err := repo.Get(ctx, id.String())
		require.NoError(t, err)
		require.NotNil(t, stored.Metadata)
		assert.Equal(t, []Metadata{{Key: key, Value: value}}, *stored.Metadata)
	}

	single := newThreat("Single", "source", "tmi-tf-wh")
	require.NoError(t, repo.Create(ctx, &single))
	assertMetadata(single.Id, "source", "tmi-tf-wh")

	bulk := []Threat{newThreat("Bulk A", "run", "1"), newThreat("Bulk B", "run", "2")}
	require.NoError(t, repo.BulkCreate(ctx, bulk))
	assertMetadata(bulk[0].Id, "run", "1")
	assertMetadata(bulk[1].Id, "run", "2")
}

// SEM@0000000000000000000000000000000000000000: validate a hook-rejected metadata value on threat create surfaces as a 400, not a 500
func TestGormThreatRepository_CreateRejectedMetadataIsInvalidInput(t *testing.T) {
	db, tm := setupThreatAliasTestDB(t)
	repo := NewGormThreatRepository(db, nil, nil)

	tmUUID, err := uuid.Parse(string(tm.ID))
	require.NoError(t, err)
	md := []Metadata{{Key: "source", Value: "   "}}
	threat := &Threat{Name: "Bad metadata", ThreatType: []string{"spoofing"}, ThreatModelId: &tmUUID, Metadata: &md}

	err = repo.Create(context.Background(), threat)
	require.Error(t, err)
	assert.Equal(t, 400, WriteErrorToRequestError(err, "Failed to create threat").Status)
}

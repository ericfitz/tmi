package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1155: write responses must carry microsecond UTC timestamps, matching what
// the database stores.

func assertMicrosecondUTC(t *testing.T, name string, ts time.Time) {
	t.Helper()
	assert.Zero(t, ts.Nanosecond()%1000, "%s must carry microsecond resolution", name)
	assert.Equal(t, time.UTC, ts.Location(), "%s must be UTC", name)
}

func TestClientCredentialCreate_ReturnsMicrosecondUTCTimestamps(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()
	repo := NewGormClientCredentialRepository(tdb.DB)

	cred, err := repo.Create(context.Background(), ClientCredentialCreateParams{
		OwnerUUID: uuid.New(), ClientID: "tmi_cc_ts", ClientSecretHash: "h", Name: "n",
	})
	require.NoError(t, err)
	assertMicrosecondUTC(t, "created_at", cred.CreatedAt)
	assertMicrosecondUTC(t, "modified_at", cred.ModifiedAt)

	got, err := repo.GetByClientID(context.Background(), "tmi_cc_ts")
	require.NoError(t, err)
	assert.True(t, got.CreatedAt.Equal(cred.CreatedAt), "created_at: response %v, stored %v", cred.CreatedAt, got.CreatedAt)
	assert.True(t, got.ModifiedAt.Equal(cred.ModifiedAt), "modified_at: response %v, stored %v", cred.ModifiedAt, got.ModifiedAt)
}

func TestUserCreate_DefaultTimestampsAreMicrosecondUTC(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()
	repo := NewGormUserRepository(tdb.DB)

	user, err := repo.Create(context.Background(), &User{
		Provider: "google", ProviderUserID: "ts-user", Email: "ts@example.com", Name: "TS",
	})
	require.NoError(t, err)
	assertMicrosecondUTC(t, "created_at", user.CreatedAt)
	assertMicrosecondUTC(t, "modified_at", user.ModifiedAt)
}

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidatePreferences(t *testing.T) {
	t.Run("ValidPreferences", func(t *testing.T) {
		valid := []byte(`{
			"tmi-ux": {"theme": "dark", "locale": "en-US"},
			"tmi-cli": {"output": "json"}
		}`)
		err := validatePreferences(valid)
		assert.NoError(t, err)
	})

	t.Run("EmptyPreferences", func(t *testing.T) {
		valid := []byte(`{}`)
		err := validatePreferences(valid)
		assert.NoError(t, err)
	})

	t.Run("ValidClientKeys", func(t *testing.T) {
		// Test various valid client key patterns
		testCases := []string{
			`{"abc": {}}`,
			`{"ABC": {}}`,
			`{"abc-123": {}}`,
			`{"abc_123": {}}`,
			`{"tmi-ux": {}}`,
			`{"my_client-app": {}}`,
		}
		for _, tc := range testCases {
			err := validatePreferences([]byte(tc))
			assert.NoError(t, err, "Expected valid for: %s", tc)
		}
	})

	t.Run("InvalidClientKeys", func(t *testing.T) {
		// Test various invalid client key patterns
		testCases := []string{
			`{"invalid.key": {}}`, // dots not allowed
			`{"invalid key": {}}`, // spaces not allowed
			`{"invalid/key": {}}`, // slashes not allowed
			`{"": {}}`,            // empty key not allowed
			`{"@invalid": {}}`,    // special chars not allowed
		}
		for _, tc := range testCases {
			err := validatePreferences([]byte(tc))
			assert.Error(t, err, "Expected error for: %s", tc)
		}
	})

	t.Run("ClientKeyTooLong", func(t *testing.T) {
		// Client key must be 1-64 chars
		longKey := strings.Repeat("a", 65)
		invalid := []byte(`{"` + longKey + `": {}}`)
		err := validatePreferences(invalid)
		assert.Error(t, err)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		invalid := []byte(`{invalid json`)
		err := validatePreferences(invalid)
		assert.Error(t, err)
	})

	t.Run("ExceedsSizeLimit", func(t *testing.T) {
		// Create payload larger than 1KB (1024 bytes)
		largeValue := strings.Repeat("x", 2000)
		invalid := []byte(`{"tmi-ux": {"data": "` + largeValue + `"}}`)
		err := validatePreferences(invalid)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "1KB limit")
	})

	t.Run("TooManyClients", func(t *testing.T) {
		// Build preferences with 21 clients (max is 20)
		var clients []string
		for i := 0; i <= 20; i++ {
			clients = append(clients, `"client`+string(rune('a'+i))+`": {}`)
		}
		invalid := []byte(`{` + strings.Join(clients, ",") + `}`)
		err := validatePreferences(invalid)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "maximum 20")
	})

	t.Run("MaximumClientsAllowed", func(t *testing.T) {
		// Build preferences with exactly 20 clients (at the limit)
		var clients []string
		for i := range 20 {
			clients = append(clients, `"c`+string(rune('a'+i))+`": {}`)
		}
		valid := []byte(`{` + strings.Join(clients, ",") + `}`)
		err := validatePreferences(valid)
		assert.NoError(t, err)
	})
}

// setupUserPreferencesTestDB creates an in-memory SQLite DB with the tables
// needed for GetCurrentUserPreferences handler tests, seeded with a user.
func setupUserPreferencesTestDB(t *testing.T) (*gorm.DB, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.UserPreference{},
	))

	user := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(strPtr("preferences-test-user")),
		Email:          models.DBVarchar("prefs@example.com"),
		Name:           models.DBVarchar("Prefs User"),
	}
	require.NoError(t, db.Create(user).Error)

	return db, user
}

// TestGetCurrentUserPreferences_EmptyStoredPreferences guards against the
// zero-500 regression in #697: Oracle can hand back a zero-length CLOB for a
// stored preferences row (JSONRaw.Scan normalizes that to a nil JSONRaw), and
// json.Unmarshal on nil input previously bubbled up as a 500 instead of the
// documented 200-with-defaults response.
func TestGetCurrentUserPreferences_EmptyStoredPreferences(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, user := setupUserPreferencesTestDB(t)

	// Insert a preferences row with a genuinely zero-length stored value
	// (bypassing JSONRaw.Value, which would normalize an empty slice to NULL
	// on Create) to reproduce the empty-CLOB-read-back scenario directly.
	require.NoError(t, db.Exec(
		"INSERT INTO user_preferences (id, user_internal_uuid, preferences, created_at, modified_at) VALUES (?, ?, '', datetime('now'), datetime('now'))",
		uuid.New().String(), string(user.InternalUUID),
	).Error)

	original := ThreatModelStore
	defer func() { ThreatModelStore = original }()
	ThreatModelStore = NewGormThreatModelStore(db)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/me/preferences", nil)
	c.Set("userInternalUUID", string(user.InternalUUID))

	s := &Server{}
	s.GetCurrentUserPreferences(c)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	assert.JSONEq(t, "{}", w.Body.String())
}

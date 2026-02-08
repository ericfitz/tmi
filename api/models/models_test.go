package models

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// Auto-migrate all models
	err = db.AutoMigrate(AllModels()...)
	require.NoError(t, err)

	return db
}

func TestUser_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{
		Provider: "google",
		Email:    "test@example.com",
		Name:     "Test User",
	}

	err := db.Create(user).Error
	require.NoError(t, err)

	assert.NotEmpty(t, user.InternalUUID)
	_, err = uuid.Parse(user.InternalUUID)
	assert.NoError(t, err, "InternalUUID should be valid UUID")
}

func TestUser_BeforeCreate_PreservesExistingUUID(t *testing.T) {
	db := setupTestDB(t)

	existingUUID := uuid.New().String()
	user := &User{
		InternalUUID: existingUUID,
		Provider:     "google",
		Email:        "test@example.com",
		Name:         "Test User",
	}

	err := db.Create(user).Error
	require.NoError(t, err)

	assert.Equal(t, existingUUID, user.InternalUUID)
}

func TestRefreshTokenRecord_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	// Create a user first for the foreign key
	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	record := &RefreshTokenRecord{
		UserInternalUUID: user.InternalUUID,
		Token:            "test-token-12345",
	}

	err := db.Create(record).Error
	require.NoError(t, err)

	assert.NotEmpty(t, record.ID)
	_, err = uuid.Parse(record.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestRefreshTokenRecord_BeforeCreate_PreservesExistingUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	existingUUID := uuid.New().String()
	record := &RefreshTokenRecord{
		ID:               existingUUID,
		UserInternalUUID: user.InternalUUID,
		Token:            "test-token-12345",
	}

	err := db.Create(record).Error
	require.NoError(t, err)

	assert.Equal(t, existingUUID, record.ID)
}

func TestClientCredential_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	cred := &ClientCredential{
		OwnerUUID:        user.InternalUUID,
		ClientID:         "tmi_cc_test",
		ClientSecretHash: "hash",
		Name:             "Test Credential",
	}

	err := db.Create(cred).Error
	require.NoError(t, err)

	assert.NotEmpty(t, cred.ID)
	_, err = uuid.Parse(cred.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestThreatModel_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{
		OwnerInternalUUID:     user.InternalUUID,
		CreatedByInternalUUID: user.InternalUUID,
		Name:                  "Test TM",
	}

	err := db.Create(tm).Error
	require.NoError(t, err)

	assert.NotEmpty(t, tm.ID)
	_, err = uuid.Parse(tm.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestDiagram_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{OwnerInternalUUID: user.InternalUUID, CreatedByInternalUUID: user.InternalUUID, Name: "Test TM"}
	require.NoError(t, db.Create(tm).Error)

	diagram := &Diagram{
		ThreatModelID: tm.ID,
		Name:          "Test Diagram",
	}

	err := db.Create(diagram).Error
	require.NoError(t, err)

	assert.NotEmpty(t, diagram.ID)
	_, err = uuid.Parse(diagram.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestAsset_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{OwnerInternalUUID: user.InternalUUID, CreatedByInternalUUID: user.InternalUUID, Name: "Test TM"}
	require.NoError(t, db.Create(tm).Error)

	asset := &Asset{
		ThreatModelID: tm.ID,
		Name:          "Test Asset",
		Type:          "data", // Valid asset type from ValidAssetTypes
	}

	err := db.Create(asset).Error
	require.NoError(t, err)

	assert.NotEmpty(t, asset.ID)
	_, err = uuid.Parse(asset.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestGroup_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	group := &Group{
		Provider:  "google",
		GroupName: "test-group",
	}

	err := db.Create(group).Error
	require.NoError(t, err)

	assert.NotEmpty(t, group.InternalUUID)
	_, err = uuid.Parse(group.InternalUUID)
	assert.NoError(t, err, "InternalUUID should be valid UUID")
}

func TestGroup_BeforeCreate_PreservesExistingUUID(t *testing.T) {
	db := setupTestDB(t)

	existingUUID := uuid.New().String()
	group := &Group{
		InternalUUID: existingUUID,
		Provider:     "google",
		GroupName:    "test-group",
	}

	err := db.Create(group).Error
	require.NoError(t, err)

	assert.Equal(t, existingUUID, group.InternalUUID)
}

func TestThreatModelAccess_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{OwnerInternalUUID: user.InternalUUID, CreatedByInternalUUID: user.InternalUUID, Name: "Test TM"}
	require.NoError(t, db.Create(tm).Error)

	access := &ThreatModelAccess{
		ThreatModelID:    tm.ID,
		UserInternalUUID: &user.InternalUUID,
		SubjectType:      "user",
		Role:             "owner",
	}

	err := db.Create(access).Error
	require.NoError(t, err)

	assert.NotEmpty(t, access.ID)
	_, err = uuid.Parse(access.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestDocument_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{OwnerInternalUUID: user.InternalUUID, CreatedByInternalUUID: user.InternalUUID, Name: "Test TM"}
	require.NoError(t, db.Create(tm).Error)

	doc := &Document{
		ThreatModelID: tm.ID,
		Name:          "Test Doc",
		URI:           "https://example.com/doc",
	}

	err := db.Create(doc).Error
	require.NoError(t, err)

	assert.NotEmpty(t, doc.ID)
	_, err = uuid.Parse(doc.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestNote_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{OwnerInternalUUID: user.InternalUUID, CreatedByInternalUUID: user.InternalUUID, Name: "Test TM"}
	require.NoError(t, db.Create(tm).Error)

	note := &Note{
		ThreatModelID: tm.ID,
		Name:          "Test Note",
		Content:       "Test content",
	}

	err := db.Create(note).Error
	require.NoError(t, err)

	assert.NotEmpty(t, note.ID)
	_, err = uuid.Parse(note.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestRepository_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{OwnerInternalUUID: user.InternalUUID, CreatedByInternalUUID: user.InternalUUID, Name: "Test TM"}
	require.NoError(t, db.Create(tm).Error)

	repo := &Repository{
		ThreatModelID: tm.ID,
		URI:           "https://github.com/example/repo",
	}

	err := db.Create(repo).Error
	require.NoError(t, err)

	assert.NotEmpty(t, repo.ID)
	_, err = uuid.Parse(repo.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestCollaborationSession_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{OwnerInternalUUID: user.InternalUUID, CreatedByInternalUUID: user.InternalUUID, Name: "Test TM"}
	require.NoError(t, db.Create(tm).Error)

	diagram := &Diagram{ThreatModelID: tm.ID, Name: "Test Diagram"}
	require.NoError(t, db.Create(diagram).Error)

	session := &CollaborationSession{
		ThreatModelID: tm.ID,
		DiagramID:     diagram.ID,
		WebsocketURL:  "wss://example.com/ws",
	}

	err := db.Create(session).Error
	require.NoError(t, err)

	assert.NotEmpty(t, session.ID)
	_, err = uuid.Parse(session.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestSessionParticipant_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	tm := &ThreatModel{OwnerInternalUUID: user.InternalUUID, CreatedByInternalUUID: user.InternalUUID, Name: "Test TM"}
	require.NoError(t, db.Create(tm).Error)

	diagram := &Diagram{ThreatModelID: tm.ID, Name: "Test Diagram"}
	require.NoError(t, db.Create(diagram).Error)

	session := &CollaborationSession{ThreatModelID: tm.ID, DiagramID: diagram.ID, WebsocketURL: "wss://example.com/ws"}
	require.NoError(t, db.Create(session).Error)

	participant := &SessionParticipant{
		SessionID:        session.ID,
		UserInternalUUID: user.InternalUUID,
	}

	err := db.Create(participant).Error
	require.NoError(t, err)

	assert.NotEmpty(t, participant.ID)
	_, err = uuid.Parse(participant.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestWebhookSubscription_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	webhook := &WebhookSubscription{
		OwnerInternalUUID: user.InternalUUID,
		Name:              "Test Webhook",
		URL:               "https://example.com/webhook",
		Events:            StringArray{"event.created"},
		Status:            "pending_verification", // Valid webhook status
	}

	err := db.Create(webhook).Error
	require.NoError(t, err)

	assert.NotEmpty(t, webhook.ID)
	_, err = uuid.Parse(webhook.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestWebhookURLDenyList_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	denyEntry := &WebhookURLDenyList{
		Pattern:     "*.internal.local",
		PatternType: "glob",
	}

	err := db.Create(denyEntry).Error
	require.NoError(t, err)

	assert.NotEmpty(t, denyEntry.ID)
	_, err = uuid.Parse(denyEntry.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestAddon_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	webhook := &WebhookSubscription{
		OwnerInternalUUID: user.InternalUUID,
		Name:              "Addon Webhook",
		URL:               "https://example.com/addon",
		Events:            StringArray{"addon.invoked"},
		Status:            "active", // Valid webhook status
	}
	require.NoError(t, db.Create(webhook).Error)

	addon := &Addon{
		Name:      "Test Addon",
		WebhookID: webhook.ID,
	}

	err := db.Create(addon).Error
	require.NoError(t, err)

	assert.NotEmpty(t, addon.ID)
	_, err = uuid.Parse(addon.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestGroupMember_BeforeCreate_GeneratesUUID(t *testing.T) {
	db := setupTestDB(t)

	user := &User{Provider: "google", Email: "test@example.com", Name: "Test"}
	require.NoError(t, db.Create(user).Error)

	group := &Group{Provider: "*", GroupName: "test-group"}
	require.NoError(t, db.Create(group).Error)

	userUUID := user.InternalUUID
	member := &GroupMember{
		GroupInternalUUID: group.InternalUUID,
		UserInternalUUID:  &userUUID,
		SubjectType:       "user",
	}

	err := db.Create(member).Error
	require.NoError(t, err)

	assert.NotEmpty(t, member.ID)
	_, err = uuid.Parse(member.ID)
	assert.NoError(t, err, "ID should be valid UUID")
}

func TestAllModels_ReturnsAllModels(t *testing.T) {
	models := AllModels()

	// 28 models (Administrator model removed, admin managed via Administrators group)
	assert.Len(t, models, 28)
}

func TestAllModels_MigratesSuccessfully(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// Should not panic or error
	err = db.AutoMigrate(AllModels()...)
	assert.NoError(t, err)
}

func TestTableNames(t *testing.T) {
	tests := []struct {
		model    interface{ TableName() string }
		expected string
	}{
		{&User{}, "users"},
		{&RefreshTokenRecord{}, "refresh_tokens"},
		{&ClientCredential{}, "client_credentials"},
		{&Group{}, "groups"},
		{&ThreatModel{}, "threat_models"},
		{&Diagram{}, "diagrams"},
		{&Asset{}, "assets"},
		{&Threat{}, "threats"},
		{&ThreatModelAccess{}, "threat_model_access"},
		{&Document{}, "documents"},
		{&Note{}, "notes"},
		{&Repository{}, "repositories"},
		{&Metadata{}, "metadata"},
		{&CollaborationSession{}, "collaboration_sessions"},
		{&SessionParticipant{}, "session_participants"},
		{&WebhookSubscription{}, "webhook_subscriptions"},
		{&WebhookDelivery{}, "webhook_deliveries"},
		{&WebhookQuota{}, "webhook_quotas"},
		{&WebhookURLDenyList{}, "webhook_url_deny_list"},
		{&Addon{}, "addons"},
		{&AddonInvocationQuota{}, "addon_invocation_quotas"},
		{&UserAPIQuota{}, "user_api_quotas"},
		{&GroupMember{}, "group_members"},
		{&SystemSetting{}, "system_settings"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.model.TableName())
		})
	}
}

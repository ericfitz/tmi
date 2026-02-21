package api

import (
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthServiceGetter defines an interface for getting the auth service
type AuthServiceGetter interface {
	GetService() *auth.Service
}

// WithTimestamps is a mixin interface for entities with timestamps
type WithTimestamps interface {
	SetCreatedAt(time.Time)
	SetModifiedAt(time.Time)
}

// UpdateTimestamps updates the timestamps on an entity
func UpdateTimestamps[T WithTimestamps](entity T, isNew bool) T {
	now := time.Now().UTC()
	if isNew {
		entity.SetCreatedAt(now)
	}
	entity.SetModifiedAt(now)
	return entity
}

// Store interfaces to allow switching between in-memory and database implementations
// ThreatModelWithCounts extends ThreatModel with count information
type ThreatModelWithCounts struct {
	ThreatModel
	DocumentCount int
	SourceCount   int
	DiagramCount  int
	ThreatCount   int
	NoteCount     int
	AssetCount    int
}

// ThreatModelFilters defines filtering criteria for listing threat models
type ThreatModelFilters struct {
	Owner               *string    // Filter by owner email or display name (partial match)
	Name                *string    // Filter by name (partial match)
	Description         *string    // Filter by description (partial match)
	IssueUri            *string    // Filter by issue_uri (partial match)
	CreatedAfter        *time.Time // Filter by created_at >= value
	CreatedBefore       *time.Time // Filter by created_at <= value
	ModifiedAfter       *time.Time // Filter by modified_at >= value
	ModifiedBefore      *time.Time // Filter by modified_at <= value
	Status              *string    // Filter by status (exact match)
	StatusUpdatedAfter  *time.Time // Filter by status_updated >= value
	StatusUpdatedBefore *time.Time // Filter by status_updated <= value
}

type ThreatModelStoreInterface interface {
	Get(id string) (ThreatModel, error)
	List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel
	// ListWithCounts returns paginated threat models with counts and total count (before pagination)
	ListWithCounts(offset, limit int, filter func(ThreatModel) bool, filters *ThreatModelFilters) ([]ThreatModelWithCounts, int)
	Create(item ThreatModel, idSetter func(ThreatModel, string) ThreatModel) (ThreatModel, error)
	Update(id string, item ThreatModel) error
	Delete(id string) error
	Count() int
}

type DiagramStoreInterface interface {
	Get(id string) (DfdDiagram, error)
	GetThreatModelID(diagramID string) (string, error)
	List(offset, limit int, filter func(DfdDiagram) bool) []DfdDiagram
	Create(item DfdDiagram, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error)
	CreateWithThreatModel(item DfdDiagram, threatModelID string, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error)
	Update(id string, item DfdDiagram) error
	Delete(id string) error
	Count() int
}

// Global store instances (will be initialized in main.go)
var ThreatModelStore ThreatModelStoreInterface
var DiagramStore DiagramStoreInterface
var GlobalDocumentStore DocumentStore
var GlobalNoteStore NoteStore
var GlobalRepositoryStore RepositoryStore
var GlobalAssetStore AssetStore
var GlobalThreatStore ThreatStore
var GlobalMetadataStore MetadataStore
var GlobalSurveyStore SurveyStore
var GlobalSurveyResponseStore SurveyResponseStore
var GlobalTriageNoteStore TriageNoteStore

// InitializeGormStores initializes all stores with GORM implementations
// This is the only store initialization function - all databases use GORM
func InitializeGormStores(db *gorm.DB, authService any, cache *CacheService, invalidator *CacheInvalidator) {
	// Core stores
	ThreatModelStore = NewGormThreatModelStore(db)
	DiagramStore = NewGormDiagramStore(db)

	// Sub-resource stores
	GlobalDocumentStore = NewGormDocumentStore(db, cache, invalidator)
	GlobalNoteStore = NewGormNoteStore(db, cache, invalidator)
	GlobalRepositoryStore = NewGormRepositoryStore(db, cache, invalidator)
	GlobalAssetStore = NewGormAssetStore(db, cache, invalidator)
	GlobalThreatStore = NewGormThreatStore(db, cache, invalidator)
	GlobalMetadataStore = NewGormMetadataStore(db, cache, invalidator)

	// Webhook stores
	GlobalWebhookSubscriptionStore = NewGormWebhookSubscriptionStore(db)
	GlobalWebhookDeliveryStore = NewGormWebhookDeliveryStore(db)
	GlobalWebhookQuotaStore = NewGormWebhookQuotaStore(db)
	GlobalWebhookUrlDenyListStore = NewGormWebhookUrlDenyListStore(db)

	// Admin/quota stores
	GlobalUserAPIQuotaStore = NewGormUserAPIQuotaStore(db)
	GlobalAddonStore = NewGormAddonStore(db)
	GlobalGroupMemberStore = NewGormGroupMemberStore(db)
	adminDB = db
	GlobalAddonInvocationQuotaStore = NewGormAddonInvocationQuotaStore(db)

	// Survey stores
	GlobalSurveyStore = NewGormSurveyStore(db)
	GlobalSurveyResponseStore = NewGormSurveyResponseStore(db)
	GlobalTriageNoteStore = NewGormTriageNoteStore(db)

	// User/Group stores with auth service
	if authService != nil {
		if svc, ok := authService.(AuthServiceGetter); ok {
			GlobalUserStore = NewGormUserStore(db, svc.GetService())
			GlobalGroupStore = NewGormGroupStore(db, svc.GetService())
		}
	}
}

// ParseUUIDOrNil parses a UUID string, returning a nil UUID on error
func ParseUUIDOrNil(s string) uuid.UUID {
	if u, err := uuid.Parse(s); err == nil {
		return u
	}
	return uuid.Nil
}

// GetAllModels returns all GORM models for AutoMigrate
// This function is used by the server to run database migrations for non-postgres databases
func GetAllModels() []any {
	return []any{
		&models.User{},
		&models.RefreshTokenRecord{},
		&models.ClientCredential{},
		&models.ThreatModel{},
		&models.Diagram{},
		&models.Asset{},
		&models.Threat{},
		&models.Group{},
		&models.ThreatModelAccess{},
		&models.Document{},
		&models.Note{},
		&models.Repository{},
		&models.Metadata{},
		&models.CollaborationSession{},
		&models.SessionParticipant{},
		&models.WebhookSubscription{},
		&models.WebhookDelivery{},
		&models.WebhookQuota{},
		&models.WebhookURLDenyList{},
		&models.Addon{},
		&models.AddonInvocationQuota{},
		&models.UserAPIQuota{},
		&models.GroupMember{},
		&models.UserPreference{},
		&models.SystemSetting{},
		&models.SurveyTemplate{},
		&models.SurveyResponse{},
		&models.SurveyResponseAccess{},
		&models.TriageNote{},
		// Note: survey_template_versions table kept for historical data but no longer used by API
	}
}

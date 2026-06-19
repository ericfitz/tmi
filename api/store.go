package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthServiceGetter defines an interface for getting the auth service
// SEM@35d3e17459ee0834e412739eaa604652b815d559: interface for retrieving the auth service instance (pure)
type AuthServiceGetter interface {
	GetService() *auth.Service
}

// WithTimestamps is a mixin interface for entities with timestamps
// SEM@386eea01f3b66c35027bf3ca762efbc291419e20: mixin interface for entities that expose created_at and modified_at timestamp setters (pure)
type WithTimestamps interface {
	SetCreatedAt(time.Time)
	SetModifiedAt(time.Time)
}

// UpdateTimestamps updates the timestamps on an entity
// SEM@a37a0039279be689bb07be2113fe86024a410a4b: set created_at and modified_at to a microsecond-truncated UTC now on an entity (pure)
func UpdateTimestamps[T WithTimestamps](entity T, isNew bool) T {
	// Truncate to microseconds so the in-memory value matches what the database
	// persists (PostgreSQL and Oracle store microsecond precision) and conforms
	// to the OpenAPI timestamp schema, which permits at most 6 fractional
	// digits. Without this, a create response returns the Go nanosecond value
	// while a later GET returns the truncated DB value, so they appear to differ.
	now := time.Now().UTC().Truncate(time.Microsecond)
	if isNew {
		entity.SetCreatedAt(now)
	}
	entity.SetModifiedAt(now)
	return entity
}

// Store interfaces to allow switching between in-memory and database implementations

// ThreatModelFilters defines filtering criteria for listing threat models
// SEM@cd5f8ed4949685a202f3e973e6cddb10850f0f15: value type holding optional filter criteria for listing threat models (pure)
type ThreatModelFilters struct {
	Owner               *string       // Filter by owner email or display name (partial match)
	Name                *string       // Filter by name (partial match)
	Description         *string       // Filter by description (partial match)
	IssueUri            *string       // Filter by issue_uri (partial match)
	CreatedAfter        *time.Time    // Filter by created_at >= value
	CreatedBefore       *time.Time    // Filter by created_at <= value
	ModifiedAfter       *time.Time    // Filter by modified_at >= value
	ModifiedBefore      *time.Time    // Filter by modified_at <= value
	Status              []string      // Filter by status values (exact match, supports multiple)
	StatusUpdatedAfter  *time.Time    // Filter by status_updated >= value
	StatusUpdatedBefore *time.Time    // Filter by status_updated <= value
	SecurityReviewer    *ParsedFilter // Filter by security reviewer (supports operator syntax: is:null, is:notnull, or partial match)
	IncludeDeleted      bool          // Include soft-deleted (tombstoned) entities
}

// SEM@c79f3cd129aecd7cd6562b875b7f02232594d3d1: interface for CRUD, soft-delete, list, count, and authorization operations on threat models
type ThreatModelStoreInterface interface {
	Get(id string) (ThreatModel, error)
	GetIncludingDeleted(id string) (ThreatModel, error)
	GetAuthorization(id string) ([]Authorization, User, error)
	GetAuthorizationIncludingDeleted(id string) ([]Authorization, User, error)
	List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel
	// ListWithCounts returns paginated threat model list items with counts and total count (before pagination)
	ListWithCounts(offset, limit int, filter func(ThreatModel) bool, filters *ThreatModelFilters) ([]TMListItem, int)
	Create(item ThreatModel, idSetter func(ThreatModel, string) ThreatModel) (ThreatModel, error)
	// Update accepts a context.Context so the underlying retry wrapper uses
	// the caller's ctx instead of context.Background(); see #334.
	Update(ctx context.Context, id string, item ThreatModel) error
	Delete(id string) error
	// SoftDelete accepts a context.Context for retry-wrapper cancellability; see #334.
	SoftDelete(ctx context.Context, id string) error
	Restore(id string) error
	HardDelete(id string) error
	Count() int
}

// SEM@c79f3cd129aecd7cd6562b875b7f02232594d3d1: interface for CRUD, batch fetch, soft-delete, and count operations on DFD diagrams
type DiagramStoreInterface interface {
	Get(id string) (DfdDiagram, error)
	GetIncludingDeleted(id string) (DfdDiagram, error)
	GetBatch(ids []string) ([]DfdDiagram, error)
	GetThreatModelID(diagramID string) (string, error)
	List(offset, limit int, filter func(DfdDiagram) bool) []DfdDiagram
	Create(item DfdDiagram, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error)
	CreateWithThreatModel(item DfdDiagram, threatModelID string, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error)
	// Update accepts a context.Context so the underlying retry wrapper uses
	// the caller's ctx instead of context.Background(); see #334.
	Update(ctx context.Context, id string, item DfdDiagram) error
	Delete(id string) error
	// SoftDelete accepts a context.Context for retry-wrapper cancellability; see #334.
	SoftDelete(ctx context.Context, id string) error
	Restore(id string) error
	HardDelete(id string) error
	Count() int
}

// Global store instances (will be initialized in main.go)
var ThreatModelStore ThreatModelStoreInterface
var DiagramStore DiagramStoreInterface
var GlobalDocumentRepository DocumentRepository
var GlobalNoteRepository NoteRepository
var GlobalRepositoryRepository RepositoryRepository
var GlobalAssetRepository AssetRepository
var GlobalThreatRepository ThreatRepository
var GlobalSurveyStore SurveyStore
var GlobalSurveyResponseStore SurveyResponseStore
var GlobalTriageNoteStore TriageNoteStore
var GlobalSurveyAnswerStore SurveyAnswerStore
var GlobalTeamStore TeamStoreInterface
var GlobalProjectStore ProjectStoreInterface
var GlobalTeamNoteStore TeamNoteStoreInterface
var GlobalProjectNoteStore ProjectNoteStoreInterface

// Audit trail and versioning
var GlobalAuditService AuditServiceInterface
var GlobalAuditDebouncer *AuditDebouncer

// Repository globals (new typed-error implementations)
var GlobalGroupRepository GroupRepository
var GlobalMetadataRepository MetadataRepository
var GlobalGroupMemberRepository GroupMemberRepository

// Feedback repository globals
var GlobalUsabilityFeedbackRepository UsabilityFeedbackRepository
var GlobalContentFeedbackRepository ContentFeedbackRepository

// globalAuthService is used by DeleteAdminGroup to call DeleteGroupAndData.
// It is set in InitializeGormStores when an authService is provided.
var globalAuthService interface {
	DeleteGroupAndData(ctx context.Context, internalUUID string) (*auth.GroupDeletionResult, error)
}

// InitializeGormStores initializes all stores with GORM implementations
// This is the only store initialization function - all databases use GORM
// SEM@cd6b617fb7aaaeb6491d79c87b09839f94b0fc3e: initialize all global GORM-backed stores and repositories using the provided DB, auth service, and cache (mutates shared state)
func InitializeGormStores(db *gorm.DB, authService any, cache *CacheService, invalidator *CacheInvalidator) {
	// Set global cache service for middleware and other nil-guarded callers
	GlobalCacheService = cache

	// Core stores
	ThreatModelStore = NewGormThreatModelStore(db)
	DiagramStore = NewGormDiagramStore(db)

	// Sub-resource stores
	GlobalDocumentRepository = NewGormDocumentRepository(db, cache, invalidator)
	GlobalNoteRepository = NewGormNoteRepository(db, cache, invalidator)
	GlobalRepositoryRepository = NewGormRepositoryRepository(db, cache, invalidator)
	GlobalAssetRepository = NewGormAssetRepository(db, cache, invalidator)
	GlobalThreatRepository = NewGormThreatRepository(db, cache, invalidator)
	GlobalMetadataRepository = NewGormMetadataRepository(db, cache, invalidator)

	// Webhook stores
	GlobalWebhookSubscriptionStore = NewGormWebhookSubscriptionStore(db)
	GlobalWebhookQuotaStore = NewGormWebhookQuotaStore(db)
	GlobalWebhookUrlDenyListStore = NewGormWebhookUrlDenyListStore(db)

	// Admin/quota stores
	GlobalUserAPIQuotaStore = NewGormUserAPIQuotaStore(db)
	GlobalAddonStore = NewGormAddonStore(db)
	GlobalGroupMemberRepository = NewGormGroupMemberRepository(db)
	GlobalGroupRepository = NewGormGroupRepository(db)
	adminDB = db
	GlobalAddonInvocationQuotaStore = NewGormAddonInvocationQuotaStore(db)

	// Feedback repositories
	GlobalUsabilityFeedbackRepository = NewGormUsabilityFeedbackRepository(db)
	GlobalContentFeedbackRepository = NewGormContentFeedbackRepository(db)

	// Survey stores
	GlobalSurveyStore = NewGormSurveyStore(db)
	GlobalSurveyResponseStore = NewGormSurveyResponseStore(db)
	GlobalTriageNoteStore = NewGormTriageNoteStore(db)
	GlobalSurveyAnswerStore = NewGormSurveyAnswerStore(db)

	// Team/Project stores
	GlobalTeamStore = NewGormTeamStore(db)
	GlobalProjectStore = NewGormProjectStore(db)
	GlobalTeamNoteStore = NewGormTeamNoteStore(db)
	GlobalProjectNoteStore = NewGormProjectNoteStore(db)

	// Audit trail and versioning
	GlobalAuditService = NewGormAuditService(db)
	GlobalAuditDebouncer = NewAuditDebouncer(GlobalAuditService)
	SetTeamAuthDB(db)

	// User/Group stores with auth service
	if authService != nil {
		if svc, ok := authService.(AuthServiceGetter); ok {
			GlobalUserStore = NewGormUserStore(db, svc.GetService())
			globalAuthService = svc.GetService()
		}
	}

	// Timmy stores
	GlobalTimmyEmbeddingStore = NewGormTimmyEmbeddingStore(db)
	GlobalTimmySessionStore = NewGormTimmySessionStore(db)
	GlobalTimmyMessageStore = NewGormTimmyMessageStore(db)
	GlobalTimmyUsageStore = NewGormTimmyUsageStore(db)
}

// ParseUUIDOrNil parses a UUID string, returning a nil UUID on error
// SEM@09b9acb42bb2ed2bd519ff1f962213011e015b62: parse a UUID string and return uuid.Nil on parse failure (pure)
func ParseUUIDOrNil(s string) uuid.UUID {
	if u, err := uuid.Parse(s); err == nil {
		return u
	}
	return uuid.Nil
}

// GetAllModels returns all GORM models for AutoMigrate
// This function is used by the server to run database migrations for non-postgres databases
// SEM@45a055dc8bd72a23aefc3c2edcf48d64d511ff36: return all GORM model instances for AutoMigrate (pure)
func GetAllModels() []any {
	return models.AllModels()
}

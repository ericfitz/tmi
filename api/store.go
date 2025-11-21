package api

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

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

type ThreatModelStoreInterface interface {
	Get(id string) (ThreatModel, error)
	List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel
	ListWithCounts(offset, limit int, filter func(ThreatModel) bool) []ThreatModelWithCounts
	Create(item ThreatModel, idSetter func(ThreatModel, string) ThreatModel) (ThreatModel, error)
	Update(id string, item ThreatModel) error
	Delete(id string) error
	Count() int
}

type DiagramStoreInterface interface {
	Get(id string) (DfdDiagram, error)
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

// InitializeDatabaseStores initializes stores with database implementations
func InitializeDatabaseStores(db *sql.DB) {
	ThreatModelStore = NewThreatModelDatabaseStore(db)
	DiagramStore = NewDiagramDatabaseStore(db)
	GlobalDocumentStore = NewDatabaseDocumentStore(db, nil, nil)
	GlobalNoteStore = NewDatabaseNoteStore(db, nil, nil)
	GlobalRepositoryStore = NewDatabaseRepositoryStore(db, nil, nil)
	GlobalAssetStore = NewDatabaseAssetStore(db, nil, nil)
	GlobalThreatStore = NewDatabaseThreatStore(db, nil, nil)
	GlobalMetadataStore = NewDatabaseMetadataStore(db, nil, nil)
	GlobalWebhookSubscriptionStore = NewDBWebhookSubscriptionDatabaseStore(db)
	GlobalWebhookDeliveryStore = NewDBWebhookDeliveryDatabaseStore(db)
	GlobalWebhookQuotaStore = NewWebhookQuotaDatabaseStore(db)
	GlobalWebhookUrlDenyListStore = NewWebhookUrlDenyListDatabaseStore(db)
	GlobalUserAPIQuotaStore = NewUserAPIQuotaDatabaseStore(db)
}

// NOTE: InitializeInMemoryStores function removed - all stores now use database implementations

// ParseUUIDOrNil parses a UUID string, returning a nil UUID on error
func ParseUUIDOrNil(s string) uuid.UUID {
	if u, err := uuid.Parse(s); err == nil {
		return u
	}
	return uuid.Nil
}

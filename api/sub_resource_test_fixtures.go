package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SubResourceTestFixtures provides comprehensive test data for sub-resource testing
type SubResourceTestFixtures struct {
	// Test users for authorization
	OwnerUser    string
	WriterUser   string
	ReaderUser   string
	ExternalUser string // User with no access

	// Test threat model
	ThreatModel   ThreatModel
	ThreatModelID string

	// Test threats
	Threat1   Threat
	Threat1ID string
	Threat2   Threat
	Threat2ID string

	// Test documents
	Document1   Document
	Document1ID string
	Document2   Document
	Document2ID string

	// Test repositories
	Repository1   Repository
	Repository1ID string
	Repository2   Repository
	Repository2ID string

	// Test metadata
	ThreatMetadata1   Metadata
	ThreatMetadata2   Metadata
	DocumentMetadata1 Metadata
	DocumentMetadata2 Metadata
	RepositoryMetadata1   Metadata
	RepositoryMetadata2   Metadata
	DiagramMetadata1  Metadata
	DiagramMetadata2  Metadata

	// Test diagram for cell testing
	Diagram   DfdDiagram
	DiagramID string
	Cell1     DfdDiagram_Cells_Item
	Cell1ID   string
	Cell2     DfdDiagram_Cells_Item
	Cell2ID   string

	// Authorization data
	Authorization []Authorization

	// Initialization flag
	Initialized bool
}

var SubResourceFixtures SubResourceTestFixtures

// InitSubResourceTestFixtures initializes comprehensive test fixtures for sub-resource testing
func InitSubResourceTestFixtures() {
	// Set up test users
	SubResourceFixtures.OwnerUser = "owner@example.com"
	SubResourceFixtures.WriterUser = "writer@example.com"
	SubResourceFixtures.ReaderUser = "reader@example.com"
	SubResourceFixtures.ExternalUser = "external@example.com"

	// Create base timestamp
	now := time.Now().UTC()

	// Create test threat model
	threatModelUUID := uuid.New()
	SubResourceFixtures.ThreatModelID = threatModelUUID.String()

	SubResourceFixtures.ThreatModel = ThreatModel{
		Id:          &threatModelUUID,
		Name:        "Test Threat Model for Sub-Resources",
		Description: stringPointer("A comprehensive threat model for testing sub-resource operations"),
		CreatedAt:   &now,
		ModifiedAt:  &now,
		Owner:       SubResourceFixtures.OwnerUser,
		Authorization: []Authorization{
			{Subject: SubResourceFixtures.OwnerUser, Role: RoleOwner},
			{Subject: SubResourceFixtures.WriterUser, Role: RoleWriter},
			{Subject: SubResourceFixtures.ReaderUser, Role: RoleReader},
		},
	}

	// Create test threats
	threat1UUID := uuid.New()
	threat2UUID := uuid.New()
	SubResourceFixtures.Threat1ID = threat1UUID.String()
	SubResourceFixtures.Threat2ID = threat2UUID.String()

	SubResourceFixtures.Threat1 = Threat{
		Id:            &threat1UUID,
		Name:          "SQL Injection Vulnerability",
		Description:   stringPointer("Database injection through malicious SQL queries"),
		CreatedAt:     &now,
		ModifiedAt:    &now,
		ThreatModelId: &threatModelUUID,
		Severity:      ThreatSeverityHigh,
		Priority:      "high",
		Status:        "active",
		ThreatType:    "Injection",
		Mitigated:     false,
	}

	// Create a later timestamp for threat2
	laterTime := now.Add(time.Minute)
	SubResourceFixtures.Threat2 = Threat{
		Id:            &threat2UUID,
		Name:          "Cross-Site Scripting (XSS)",
		Description:   stringPointer("Client-side script injection vulnerability"),
		CreatedAt:     &laterTime,
		ModifiedAt:    &laterTime,
		ThreatModelId: &threatModelUUID,
		Severity:      ThreatSeverityMedium,
		Priority:      "medium",
		Status:        "identified",
		ThreatType:    "Cross-Site Scripting",
		Mitigated:     false,
	}

	// Create test documents
	doc1UUID := uuid.New()
	doc2UUID := uuid.New()
	SubResourceFixtures.Document1ID = doc1UUID.String()
	SubResourceFixtures.Document2ID = doc2UUID.String()

	SubResourceFixtures.Document1 = Document{
		Id:          &doc1UUID,
		Name:        "Security Requirements Document",
		Description: stringPointer("Detailed security requirements and compliance standards"),
		Uri:         stringPointer("https://docs.internal.com/security-requirements"),
	}

	SubResourceFixtures.Document2 = Document{
		Id:          &doc2UUID,
		Name:        "Architecture Design Document",
		Description: stringPointer("System architecture and design specifications"),
		Uri:         stringPointer("https://docs.internal.com/architecture-design"),
	}

	// Create test repositories
	repository1UUID := uuid.New()
	repository2UUID := uuid.New()
	SubResourceFixtures.Repository1ID = repository1UUID.String()
	SubResourceFixtures.Repository2ID = repository2UUID.String()

	gitType := Git
	SubResourceFixtures.Repository1 = Repository{
		Id:          &repository1UUID,
		Name:        stringPointer("Authentication Service"),
		Description: stringPointer("Core authentication and authorization service"),
		Uri:         stringPointer("https://github.com/company/auth-service"),
		Type:        &gitType,
	}

	SubResourceFixtures.Repository2 = Repository{
		Id:          &repository2UUID,
		Name:        stringPointer("Database Layer"),
		Description: stringPointer("Database access layer and ORM implementation"),
		Uri:         stringPointer("https://github.com/company/db-layer"),
		Type:        &gitType,
	}

	// Create test metadata
	SubResourceFixtures.ThreatMetadata1 = Metadata{
		Key:   "priority",
		Value: "high",
	}

	SubResourceFixtures.ThreatMetadata2 = Metadata{
		Key:   "review_status",
		Value: "pending",
	}

	SubResourceFixtures.DocumentMetadata1 = Metadata{
		Key:   "classification",
		Value: "internal",
	}

	SubResourceFixtures.DocumentMetadata2 = Metadata{
		Key:   "version",
		Value: "1.2",
	}

	SubResourceFixtures.RepositoryMetadata1 = Metadata{
		Key:   "language",
		Value: "go",
	}

	SubResourceFixtures.RepositoryMetadata2 = Metadata{
		Key:   "coverage",
		Value: "85%",
	}

	SubResourceFixtures.DiagramMetadata1 = Metadata{
		Key:   "version",
		Value: "2.1",
	}

	SubResourceFixtures.DiagramMetadata2 = Metadata{
		Key:   "complexity",
		Value: "medium",
	}

	// Create test diagram with cells
	diagramUUID := uuid.New()
	SubResourceFixtures.DiagramID = diagramUUID.String()

	// Create test cells
	cell1UUID := uuid.New()
	cell2UUID := uuid.New()
	SubResourceFixtures.Cell1ID = cell1UUID.String()
	SubResourceFixtures.Cell2ID = cell2UUID.String()

	// Create nodes for testing
	cell1, _ := CreateNode(SubResourceFixtures.Cell1ID, Process, 100, 200, 80, 40)
	cell2, _ := CreateNode(SubResourceFixtures.Cell2ID, Store, 300, 200, 80, 40)

	SubResourceFixtures.Cell1 = cell1
	SubResourceFixtures.Cell2 = cell2

	cells := []DfdDiagram_Cells_Item{cell1, cell2}

	SubResourceFixtures.Diagram = DfdDiagram{
		Id:         &diagramUUID,
		Name:       "Test Data Flow Diagram",
		CreatedAt:  now,
		ModifiedAt: now,
		Cells:      cells,
		Type:       DfdDiagramTypeDFD100,
	}

	// Store authorization data
	SubResourceFixtures.Authorization = []Authorization{
		{Subject: SubResourceFixtures.OwnerUser, Role: RoleOwner},
		{Subject: SubResourceFixtures.WriterUser, Role: RoleWriter},
		{Subject: SubResourceFixtures.ReaderUser, Role: RoleReader},
	}

	SubResourceFixtures.Initialized = true
}

// ResetSubResourceStores clears all sub-resource stores for testing
func ResetSubResourceStores() {
	// This function would reset stores if they were global
	// Implementation depends on store initialization patterns
}

// CreateTestThreatWithMetadata creates a threat with associated metadata for testing
func CreateTestThreatWithMetadata(threatModelID string, metadata []Metadata) Threat {
	threatUUID := uuid.New()
	threatModelTypedUUID, _ := uuid.Parse(threatModelID)

	now := time.Now().UTC()
	return Threat{
		Id:            &threatUUID,
		Name:          "Test Threat",
		Description:   stringPointer("A test threat for unit testing"),
		CreatedAt:     &now,
		ModifiedAt:    &now,
		ThreatModelId: &threatModelTypedUUID,
		Metadata:      &metadata,
		Severity:      ThreatSeverityMedium,
		Priority:      "Medium",
		Status:        "Open",
		ThreatType:    "Test",
		Mitigated:     false,
	}
}

// CreateTestDocumentWithMetadata creates a document with associated metadata for testing
func CreateTestDocumentWithMetadata(metadata []Metadata) Document {
	docUUID := uuid.New()

	return Document{
		Id:          &docUUID,
		Name:        "Test Document",
		Description: stringPointer("A test document for unit testing"),
		Uri:         stringPointer("https://test.example.com/doc"),
		Metadata:    &metadata,
	}
}

// CreateTestRepositoryWithMetadata creates a repository with associated metadata for testing
func CreateTestRepositoryWithMetadata(metadata []Metadata) Repository {
	repositoryUUID := uuid.New()

	gitType := Git
	return Repository{
		Id:          &repositoryUUID,
		Name:        stringPointer("Test Repository"),
		Description: stringPointer("A test repository for unit testing"),
		Uri:         stringPointer("https://github.com/test/repo"),
		Type:        &gitType,
		Metadata:    &metadata,
	}
}

// SetupStoresWithFixtures initializes stores with test fixtures
func SetupStoresWithFixtures(ctx context.Context) error {
	if !SubResourceFixtures.Initialized {
		InitSubResourceTestFixtures()
	}

	// This would populate stores with fixture data
	// Implementation depends on store interfaces and initialization patterns
	return nil
}

// CleanupTestFixtures removes all test data from stores
func CleanupTestFixtures(ctx context.Context) error {
	// This would clean up test data from stores
	// Implementation depends on store interfaces
	return nil
}

// AssertThreatEqual compares two threats for testing equality
func AssertThreatEqual(t1, t2 Threat) bool {
	return t1.Name == t2.Name &&
		compareStringPointers(t1.Description, t2.Description) &&
		t1.Severity == t2.Severity &&
		t1.Priority == t2.Priority &&
		t1.Status == t2.Status
}

// AssertDocumentEqual compares two documents for testing equality
func AssertDocumentEqual(d1, d2 Document) bool {
	return d1.Name == d2.Name &&
		compareStringPointers(d1.Description, d2.Description) &&
		compareStringPointers(d1.Uri, d2.Uri)
}

// AssertRepositoryEqual compares two repositories for testing equality
func AssertRepositoryEqual(r1, r2 Repository) bool {
	return compareStringPointers(r1.Name, r2.Name) &&
		compareStringPointers(r1.Description, r2.Description) &&
		compareStringPointers(r1.Uri, r2.Uri) &&
		compareRepositoryTypes(r1.Type, r2.Type)
}

// compareRepositoryTypes compares two RepositoryType pointers
func compareRepositoryTypes(t1, t2 *RepositoryType) bool {
	if t1 == nil && t2 == nil {
		return true
	}
	if t1 == nil || t2 == nil {
		return false
	}
	return *t1 == *t2
}

// AssertMetadataEqual compares two metadata items for testing equality
func AssertMetadataEqual(m1, m2 Metadata) bool {
	return m1.Key == m2.Key && m1.Value == m2.Value
}

// compareStringPointers compares two string pointers, handling nil cases
func compareStringPointers(s1, s2 *string) bool {
	if s1 == nil && s2 == nil {
		return true
	}
	if s1 == nil || s2 == nil {
		return false
	}
	return *s1 == *s2
}

// GetTestUsers returns a map of test users with their roles
func GetTestUsers() map[string]string {
	return map[string]string{
		SubResourceFixtures.OwnerUser:    "owner",
		SubResourceFixtures.WriterUser:   "writer",
		SubResourceFixtures.ReaderUser:   "reader",
		SubResourceFixtures.ExternalUser: "external",
	}
}

// GetTestUserRole returns the role for a given test user
func GetTestUserRole(user string) string {
	users := GetTestUsers()
	if role, exists := users[user]; exists {
		return role
	}
	return "unknown"
}

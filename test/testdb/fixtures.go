package testdb

import (
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
)

// Fixtures contains pre-created test data
type Fixtures struct {
	Users        map[string]*models.User
	ThreatModels map[string]*models.ThreatModel
	Diagrams     map[string]*models.Diagram
	Threats      map[string]*models.Threat
	Groups       map[string]*models.Group
}

// NewFixtures creates an empty fixtures container
func NewFixtures() *Fixtures {
	return &Fixtures{
		Users:        make(map[string]*models.User),
		ThreatModels: make(map[string]*models.ThreatModel),
		Diagrams:     make(map[string]*models.Diagram),
		Threats:      make(map[string]*models.Threat),
		Groups:       make(map[string]*models.Group),
	}
}

// CreateStandardFixtures creates a standard set of test data
func (t *TestDB) CreateStandardFixtures(prefix string) (*Fixtures, error) {
	fixtures := NewFixtures()

	// Create test user
	providerUserID := prefix + "-user@tmi.local"
	testUser := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "tmi",
		ProviderUserID: &providerUserID,
		Email:          prefix + "-user@example.com",
		Name:           prefix + " Test User",
		EmailVerified:  true,
		CreatedAt:      time.Now(),
		ModifiedAt:     time.Now(),
	}
	if err := t.CreateUser(testUser); err != nil {
		return nil, fmt.Errorf("failed to create test user: %w", err)
	}
	fixtures.Users["default"] = testUser

	// Create second test user for collaboration testing
	collaboratorUserID := prefix + "-collaborator@tmi.local"
	secondUser := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "tmi",
		ProviderUserID: &collaboratorUserID,
		Email:          prefix + "-collaborator@example.com",
		Name:           prefix + " Collaborator",
		EmailVerified:  true,
		CreatedAt:      time.Now(),
		ModifiedAt:     time.Now(),
	}
	if err := t.CreateUser(secondUser); err != nil {
		return nil, fmt.Errorf("failed to create second test user: %w", err)
	}
	fixtures.Users["collaborator"] = secondUser

	// Create test threat model
	testTM := &models.ThreatModel{
		ID:                    uuid.New().String(),
		Name:                  prefix + "-threat-model",
		Description:           new("Test threat model for integration testing"),
		OwnerInternalUUID:     testUser.InternalUUID,
		CreatedByInternalUUID: testUser.InternalUUID,
		ThreatModelFramework:  "STRIDE",
		CreatedAt:             time.Now(),
		ModifiedAt:            time.Now(),
	}
	if err := t.CreateThreatModel(testTM); err != nil {
		return nil, fmt.Errorf("failed to create test threat model: %w", err)
	}
	fixtures.ThreatModels["default"] = testTM

	// Create test diagram
	diagramType := "dfd"
	testDiagram := &models.Diagram{
		ID:            uuid.New().String(),
		ThreatModelID: testTM.ID,
		Name:          prefix + "-diagram",
		Description:   new("Test diagram for integration testing"),
		Type:          &diagramType,
		CreatedAt:     time.Now(),
		ModifiedAt:    time.Now(),
	}
	if err := t.CreateDiagram(testDiagram); err != nil {
		return nil, fmt.Errorf("failed to create test diagram: %w", err)
	}
	fixtures.Diagrams["default"] = testDiagram

	// Create test threat
	testThreat := &models.Threat{
		ID:            uuid.New().String(),
		ThreatModelID: testTM.ID,
		Name:          prefix + "-threat",
		Description:   new("Test threat for integration testing"),
		ThreatType:    models.StringArray{"Spoofing"},
		Priority:      new("high"),
		Status:        new("identified"),
		CreatedAt:     time.Now(),
		ModifiedAt:    time.Now(),
	}
	if err := t.CreateThreat(testThreat); err != nil {
		return nil, fmt.Errorf("failed to create test threat: %w", err)
	}
	fixtures.Threats["default"] = testThreat

	return fixtures, nil
}

// CreateMinimalFixtures creates only the minimum required data (one user)
func (t *TestDB) CreateMinimalFixtures(prefix string) (*Fixtures, error) {
	fixtures := NewFixtures()

	// Create test user
	providerUserID := prefix + "-user@tmi.local"
	testUser := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "tmi",
		ProviderUserID: &providerUserID,
		Email:          prefix + "-user@example.com",
		Name:           prefix + " Test User",
		EmailVerified:  true,
		CreatedAt:      time.Now(),
		ModifiedAt:     time.Now(),
	}
	if err := t.CreateUser(testUser); err != nil {
		return nil, fmt.Errorf("failed to create test user: %w", err)
	}
	fixtures.Users["default"] = testUser

	return fixtures, nil
}

// CleanupFixtures removes all entities created in the fixtures
func (t *TestDB) CleanupFixtures(fixtures *Fixtures) error {
	// Delete threats first (child of threat model)
	for _, threat := range fixtures.Threats {
		if err := t.DeleteThreat(threat.ID); err != nil {
			// Log but continue - entity might already be deleted
			continue
		}
	}

	// Delete diagrams (child of threat model)
	for _, diagram := range fixtures.Diagrams {
		if err := t.DeleteDiagram(diagram.ID); err != nil {
			continue
		}
	}

	// Delete threat models
	for _, tm := range fixtures.ThreatModels {
		if err := t.DeleteThreatModel(tm.ID); err != nil {
			continue
		}
	}

	// Delete users last
	for _, user := range fixtures.Users {
		if err := t.DeleteUser(user.InternalUUID); err != nil {
			continue
		}
	}

	return nil
}

// UserBuilder provides a fluent interface for creating test users
type UserBuilder struct {
	user *models.User
}

// NewUserBuilder creates a new user builder with default values
func NewUserBuilder(prefix string) *UserBuilder {
	providerUserID := prefix + "-user@tmi.local"
	return &UserBuilder{
		user: &models.User{
			InternalUUID:   uuid.New().String(),
			Provider:       "tmi",
			ProviderUserID: &providerUserID,
			Email:          prefix + "-user@example.com",
			Name:           prefix + " User",
			EmailVerified:  true,
			CreatedAt:      time.Now(),
			ModifiedAt:     time.Now(),
		},
	}
}

// WithProvider sets the provider
func (b *UserBuilder) WithProvider(provider string) *UserBuilder {
	b.user.Provider = provider
	return b
}

// WithEmail sets the email
func (b *UserBuilder) WithEmail(email string) *UserBuilder {
	b.user.Email = email
	return b
}

// WithName sets the name
func (b *UserBuilder) WithName(name string) *UserBuilder {
	b.user.Name = name
	return b
}

// WithProviderUserID sets the provider user ID
func (b *UserBuilder) WithProviderUserID(id string) *UserBuilder {
	b.user.ProviderUserID = &id
	return b
}

// Build returns the built user
func (b *UserBuilder) Build() *models.User {
	return b.user
}

// ThreatModelBuilder provides a fluent interface for creating test threat models
type ThreatModelBuilder struct {
	tm *models.ThreatModel
}

// NewThreatModelBuilder creates a new threat model builder with default values
func NewThreatModelBuilder(prefix string, ownerInternalUUID string) *ThreatModelBuilder {
	return &ThreatModelBuilder{
		tm: &models.ThreatModel{
			ID:                    uuid.New().String(),
			Name:                  prefix + "-threat-model",
			Description:           new("Test threat model"),
			OwnerInternalUUID:     ownerInternalUUID,
			CreatedByInternalUUID: ownerInternalUUID,
			ThreatModelFramework:  "STRIDE",
			CreatedAt:             time.Now(),
			ModifiedAt:            time.Now(),
		},
	}
}

// WithName sets the name
func (b *ThreatModelBuilder) WithName(name string) *ThreatModelBuilder {
	b.tm.Name = name
	return b
}

// WithDescription sets the description
func (b *ThreatModelBuilder) WithDescription(desc string) *ThreatModelBuilder {
	b.tm.Description = &desc
	return b
}

// WithFramework sets the framework
func (b *ThreatModelBuilder) WithFramework(fw string) *ThreatModelBuilder {
	b.tm.ThreatModelFramework = fw
	return b
}

// Build returns the built threat model
func (b *ThreatModelBuilder) Build() *models.ThreatModel {
	return b.tm
}

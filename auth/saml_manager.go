package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	crewjamsaml "github.com/crewjam/saml"
	"github.com/ericfitz/tmi/auth/saml"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// SAMLManager manages SAML providers
type SAMLManager struct {
	providers  map[string]*saml.SAMLProvider
	mu         sync.RWMutex
	service    *Service
	stateStore StateStore
}

// NewSAMLManager creates a new SAML manager
func NewSAMLManager(service *Service) *SAMLManager {
	return &SAMLManager{
		providers: make(map[string]*saml.SAMLProvider),
		service:   service,
	}
}

// InitializeProviders initializes all configured SAML providers
func (m *SAMLManager) InitializeProviders(config SAMLConfig, stateStore StateStore) error {
	logger := slogging.Get()

	if !config.Enabled {
		logger.Info("SAML authentication is disabled")
		return nil
	}

	m.stateStore = stateStore
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, providerConfig := range config.Providers {
		if !providerConfig.Enabled {
			logger.Info("SAML provider %s is disabled", id)
			continue
		}

		// Convert config to SAML config
		samlConfig := &saml.SAMLConfig{
			ID:                 id,
			Name:               providerConfig.Name,
			Enabled:            providerConfig.Enabled,
			Icon:               "fa-solid fa-key", // Default SAML icon
			EntityID:           providerConfig.EntityID,
			ACSURL:             providerConfig.ACSURL,
			SLOURL:             providerConfig.SLOURL,
			SPPrivateKey:       providerConfig.SPPrivateKey,
			SPPrivateKeyPath:   providerConfig.SPPrivateKeyPath,
			SPCertificate:      providerConfig.SPCertificate,
			SPCertificatePath:  providerConfig.SPCertificatePath,
			IDPMetadataURL:     providerConfig.IDPMetadataURL,
			IDPMetadataXML:     providerConfig.IDPMetadataXML,
			AllowIDPInitiated:  providerConfig.AllowIDPInitiated,
			ForceAuthn:         providerConfig.ForceAuthn,
			SignRequests:       providerConfig.SignRequests,
			GroupAttributeName: providerConfig.GroupsAttribute,
			AttributeMapping: map[string]string{
				"email": providerConfig.EmailAttribute,
				"name":  providerConfig.NameAttribute,
			},
		}

		// Create SAML provider
		provider, err := saml.NewSAMLProvider(samlConfig)
		if err != nil {
			logger.Error("Failed to initialize SAML provider %s: %v", id, err)
			continue
		}

		m.providers[id] = provider

		logger.Info("Initialized SAML provider: %s", id)
	}

	if len(m.providers) == 0 && config.Enabled {
		return fmt.Errorf("no SAML providers were successfully initialized")
	}

	return nil
}

// GetProvider returns a SAML provider by ID
func (m *SAMLManager) GetProvider(id string) (*saml.SAMLProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	provider, exists := m.providers[id]
	if !exists {
		return nil, fmt.Errorf("SAML provider %s not found", id)
	}

	return provider, nil
}

// ListProviders returns a list of configured SAML provider IDs
func (m *SAMLManager) ListProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.providers))
	for id := range m.providers {
		ids = append(ids, id)
	}
	return ids
}

// ProcessSAMLResponse processes a SAML response for any provider
func (m *SAMLManager) ProcessSAMLResponse(ctx context.Context, providerID string, samlResponse string, relayState string) (*User, *TokenPair, error) {
	provider, err := m.GetProvider(providerID)
	if err != nil {
		return nil, nil, err
	}

	// Parse and validate SAML response
	assertion, err := provider.ParseResponse(samlResponse)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse SAML response: %w", err)
	}

	// Extract user info from assertion
	userInfo, err := provider.ExtractUserInfoFromAssertion(assertion)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract user info: %w", err)
	}

	// Create or update user
	user, err := m.processUser(ctx, userInfo, providerID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process user: %w", err)
	}

	// Extract and cache groups
	groups := extractGroups(assertion, provider.GetConfig())
	if len(groups) > 0 {
		user.Groups = groups
		if err := m.service.CacheUserGroups(ctx, user.Email, providerID, groups); err != nil {
			// Log but don't fail
			logger := slogging.Get()
			logger.Warn("Failed to cache user groups: %v", err)
		}
	}

	// Generate tokens
	// Convert saml.UserInfo to auth.UserInfo for token generation
	authUserInfo := &UserInfo{
		ID:            userInfo.ID,
		Email:         userInfo.Email,
		EmailVerified: userInfo.EmailVerified,
		Name:          userInfo.Name,
		GivenName:     userInfo.GivenName,
		FamilyName:    userInfo.FamilyName,
		Picture:       userInfo.Picture,
		Locale:        userInfo.Locale,
		IdP:           userInfo.IdP,
		Groups:        userInfo.Groups,
	}
	tokenPair, err := m.service.GenerateTokensWithUserInfo(ctx, *user, authUserInfo)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate tokens: %w", err)
	}

	return user, &tokenPair, nil
}

// processUser creates or updates a user from SAML assertion
func (m *SAMLManager) processUser(ctx context.Context, userInfo *saml.UserInfo, providerID string) (*User, error) {
	// Check if user exists
	existingUser, err := m.service.GetUserByEmail(ctx, userInfo.Email)
	if err == nil {
		// User exists, update their info
		existingUser.Name = userInfo.Name
		existingUser.IdentityProvider = providerID
		existingUser.ModifiedAt = time.Now()

		if err := m.service.UpdateUser(ctx, existingUser); err != nil {
			return nil, fmt.Errorf("failed to update user: %w", err)
		}

		return &existingUser, nil
	}

	// Create new user
	newUser := User{
		ID:               uuid.New().String(),
		Email:            userInfo.Email,
		Name:             userInfo.Name,
		IdentityProvider: providerID,
		EmailVerified:    true, // SAML assertions are considered verified
		CreatedAt:        time.Now(),
		ModifiedAt:       time.Now(),
	}

	createdUser, err := m.service.CreateUser(ctx, newUser)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &createdUser, nil
}

// extractGroups extracts group memberships from SAML assertion
func extractGroups(assertion *crewjamsaml.Assertion, config *saml.SAMLConfig) []string {
	var groups []string

	// Look for group attribute in assertion
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			// Check if this is the groups attribute
			if attr.Name == config.GroupAttributeName ||
				attr.FriendlyName == config.GroupAttributeName ||
				attr.Name == "groups" ||
				attr.Name == "memberOf" {
				// Extract all values as groups
				for _, value := range attr.Values {
					if value.Value != "" {
						groups = append(groups, value.Value)
					}
				}
			}
		}
	}

	return groups
}

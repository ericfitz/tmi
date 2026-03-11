package api

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Server is the main API server instance
type Server struct {
	// Handlers
	threatModelHandler *ThreatModelHandler
	documentHandler    *DocumentSubResourceHandler
	noteHandler        *NoteSubResourceHandler
	repositoryHandler  *RepositorySubResourceHandler
	assetHandler       *AssetSubResourceHandler
	threatHandler      *ThreatSubResourceHandler
	triageNoteHandler  *TriageNoteSubResourceHandler
	// Generic metadata handlers for all entity types
	diagramMetadata          *GenericMetadataHandler
	documentMetadata         *GenericMetadataHandler
	noteMetadata             *GenericMetadataHandler
	repositoryMetadata       *GenericMetadataHandler
	assetMetadata            *GenericMetadataHandler
	threatMetadata           *GenericMetadataHandler
	threatModelMetadata      *GenericMetadataHandler
	surveyMetadata           *GenericMetadataHandler
	surveyResponseMetadata   *GenericMetadataHandler
	teamMetadata             *GenericMetadataHandler
	projectMetadata          *GenericMetadataHandler
	userDeletionHandler      *UserDeletionHandler
	ownershipTransferHandler *OwnershipTransferHandler
	// Audit trail handler
	auditHandler *AuditHandler
	auditPruner  *AuditPruner
	// WebSocket hub
	wsHub *WebSocketHub
	// Auth handlers (for delegating auth-related methods)
	authService AuthService // We'll need to add this dependency
	// Rate limiters
	apiRateLimiter      *APIRateLimiter
	webhookRateLimiter  *WebhookRateLimiter
	ipRateLimiter       *IPRateLimiter
	authFlowRateLimiter *AuthFlowRateLimiter
	// Settings service for database-stored configuration
	settingsService *SettingsService
	// Config provider for settings migration
	configProvider ConfigProvider
}

// ConfigProvider provides access to migratable settings from configuration
type ConfigProvider interface {
	GetMigratableSettings() []MigratableSetting
}

// MigratableSetting represents a setting that can be migrated from config to database
type MigratableSetting struct {
	Key         string
	Value       string
	Type        string
	Description string
}

// NewServer creates a new API server instance
func NewServer(wsLoggingConfig slogging.WebSocketLoggingConfig, inactivityTimeout time.Duration) *Server {
	wsHub := NewWebSocketHub(wsLoggingConfig, inactivityTimeout)
	return &Server{
		threatModelHandler: NewThreatModelHandler(wsHub),
		documentHandler:    NewDocumentSubResourceHandler(GlobalDocumentStore, nil, nil, nil),
		noteHandler:        NewNoteSubResourceHandler(GlobalNoteStore, nil, nil, nil),
		repositoryHandler:  NewRepositorySubResourceHandler(GlobalRepositoryStore, nil, nil, nil),
		assetHandler:       NewAssetSubResourceHandler(GlobalAssetStore, nil, nil, nil),
		threatHandler:      NewThreatSubResourceHandler(GlobalThreatStore, nil, nil, nil),
		triageNoteHandler:  NewTriageNoteSubResourceHandler(GlobalTriageNoteStore),
		diagramMetadata:    NewGenericMetadataHandler(GlobalMetadataStore, "diagram", "diagram_id", nil),
		documentMetadata:   NewGenericMetadataHandler(GlobalMetadataStore, "document", "document_id", nil),
		noteMetadata:       NewGenericMetadataHandler(GlobalMetadataStore, "note", "note_id", nil),
		repositoryMetadata: NewGenericMetadataHandler(GlobalMetadataStore, "repository", "repository_id", nil),
		assetMetadata:      NewGenericMetadataHandler(GlobalMetadataStore, "asset", "asset_id", nil),
		threatMetadata:     NewGenericMetadataHandler(GlobalMetadataStore, "threat", "threat_id", nil),
		threatModelMetadata: NewGenericMetadataHandler(GlobalMetadataStore, "threat_model", "threat_model_id",
			func(ctx context.Context, id uuid.UUID) error {
				_, err := ThreatModelStore.Get(id.String())
				return err
			}),
		surveyMetadata: NewGenericMetadataHandler(GlobalMetadataStore, "survey", "survey_id",
			func(ctx context.Context, id uuid.UUID) error {
				survey, err := GlobalSurveyStore.Get(ctx, id)
				if err != nil {
					return err
				}
				if survey == nil {
					return fmt.Errorf("survey not found")
				}
				return nil
			}),
		surveyResponseMetadata: NewGenericMetadataHandler(GlobalMetadataStore, "survey_response", "survey_response_id",
			func(ctx context.Context, id uuid.UUID) error {
				resp, err := GlobalSurveyResponseStore.Get(ctx, id)
				if err != nil {
					return err
				}
				if resp == nil {
					return fmt.Errorf("survey response not found")
				}
				return nil
			}),
		teamMetadata:    NewGenericMetadataHandler(GlobalMetadataStore, "team", "team_id", teamExistsFunc),
		projectMetadata: NewGenericMetadataHandler(GlobalMetadataStore, "project", "project_id", projectExistsFunc),
		wsHub:           wsHub,
		auditHandler:    NewAuditHandler(GlobalAuditService),
		auditPruner:     NewAuditPruner(GlobalAuditService),
		// authService will be set separately via SetAuthService
	}
}

// NewServerForTests creates a server with default test configuration
func NewServerForTests() *Server {
	return NewServer(slogging.WebSocketLoggingConfig{
		Enabled:        false, // Disable logging in tests by default
		RedactTokens:   true,
		MaxMessageSize: 5 * 1024,
		OnlyDebugLevel: true,
	}, 30*time.Second) // Short timeout for tests
}

// SetAuthService sets the auth service for delegating auth-related methods
func (s *Server) SetAuthService(authService AuthService) {
	s.authService = authService

	// Initialize user deletion and ownership transfer handlers with auth service
	if authAdapter, ok := authService.(*AuthServiceAdapter); ok {
		s.userDeletionHandler = NewUserDeletionHandler(authAdapter.GetService())
		s.ownershipTransferHandler = NewOwnershipTransferHandler(authAdapter.GetService())
	}
}

// SetAPIRateLimiter sets the API rate limiter
func (s *Server) SetAPIRateLimiter(rateLimiter *APIRateLimiter) {
	s.apiRateLimiter = rateLimiter
}

// SetWebhookRateLimiter sets the webhook rate limiter
func (s *Server) SetWebhookRateLimiter(rateLimiter *WebhookRateLimiter) {
	s.webhookRateLimiter = rateLimiter
}

// SetIPRateLimiter sets the IP rate limiter
func (s *Server) SetIPRateLimiter(rateLimiter *IPRateLimiter) {
	s.ipRateLimiter = rateLimiter
}

// SetAuthFlowRateLimiter sets the auth flow rate limiter
func (s *Server) SetAuthFlowRateLimiter(rateLimiter *AuthFlowRateLimiter) {
	s.authFlowRateLimiter = rateLimiter
}

// SetSettingsService sets the settings service for database-stored configuration
func (s *Server) SetSettingsService(settingsService *SettingsService) {
	s.settingsService = settingsService
}

// SetConfigProvider sets the config provider for settings migration
func (s *Server) SetConfigProvider(provider ConfigProvider) {
	s.configProvider = provider
}

// AuthService placeholder - we'll need to create this interface to avoid circular deps
type AuthService interface {
	GetProviders(c *gin.Context)
	GetSAMLProviders(c *gin.Context)
	Authorize(c *gin.Context)
	Callback(c *gin.Context)
	Exchange(c *gin.Context)
	Token(c *gin.Context)
	Refresh(c *gin.Context)
	Logout(c *gin.Context)
	RevokeToken(c *gin.Context)
	MeLogout(c *gin.Context)
	Me(c *gin.Context)
	IsValidProvider(idp string) bool
	GetProviderGroupsFromCache(ctx context.Context, idp string) ([]string, error)
}

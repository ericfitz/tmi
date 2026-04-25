package api

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
)

// SettingsServiceInterface defines the operations needed by handlers on settings.
type SettingsServiceInterface interface {
	Get(ctx context.Context, key string) (*models.SystemSetting, error)
	GetString(ctx context.Context, key string) (string, error)
	GetInt(ctx context.Context, key string) (int, error)
	GetBool(ctx context.Context, key string) (bool, error)
	List(ctx context.Context) ([]models.SystemSetting, error)
	ListByPrefix(ctx context.Context, prefix string) ([]models.SystemSetting, error)
	Set(ctx context.Context, setting *models.SystemSetting) error
	Delete(ctx context.Context, key string) error
	SeedDefaults(ctx context.Context) error
	ReEncryptAll(ctx context.Context, modifiedBy *string) (int, []SettingError, error)
}

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
	settingsService SettingsServiceInterface
	// Config provider for settings migration
	configProvider ConfigProvider
	// Provider registry for cache invalidation from settings handlers
	providerRegistry auth.ProviderRegistry
	// Ticket store for WebSocket authentication
	ticketStore TicketStore
	// Webhook configuration
	allowHTTPWebhooks bool
	// URI validators for SSRF protection
	issueURIValidator      *URIValidator
	documentURIValidator   *URIValidator
	repositoryURIValidator *URIValidator
	// Timmy AI assistant
	timmySessionManager *TimmySessionManager
	vectorManager       *VectorIndexManager
	contentPipeline     *ContentPipeline
	// Trusted proxy configuration
	trustedProxiesConfigured bool
	// Dev-mode rate limiting bypass
	rateLimitingDisabled bool
	// credentialDeleter is used by DeleteAdminUserClientCredential. When nil the handler
	// constructs a real ClientCredentialService from the auth service; set only in tests.
	credentialDeleter credentialDeleter
	// contentOAuth holds the handler for the /me/content_tokens/*,
	// /admin/users/{user_id}/content_tokens/*, and /oauth2/content_callback
	// endpoints. When nil, the delegated content provider subsystem is not
	// wired (e.g. no encryption key configured) and the six generated
	// interface methods short-circuit with 503.
	contentOAuth *ContentOAuthHandlers
	// pickerToken handles POST /me/picker_tokens/{provider_id}. When nil the
	// picker subsystem is not configured and the generated interface method
	// short-circuits with 503.
	pickerToken *PickerTokenHandler
}

// SetContentOAuthHandlers attaches the content-OAuth handler bundle used to
// service the /me/content_tokens/*, /admin/users/{user_id}/content_tokens/*,
// and /oauth2/content_callback endpoints. Called from cmd/server/main.go
// after the handler is constructed. Passing nil leaves the subsystem
// disabled — the delegation wrappers will return 503.
func (s *Server) SetContentOAuthHandlers(h *ContentOAuthHandlers) {
	s.contentOAuth = h
}

// ContentOAuthHandlers returns the attached content-OAuth handler bundle
// (nil when none is wired). Exposed so callers such as the pre-user-delete
// hook wiring can register without tripping the unused-field lint.
func (s *Server) ContentOAuthHandlers() *ContentOAuthHandlers {
	return s.contentOAuth
}

// SetPickerTokenHandler attaches the picker-token handler that services
// POST /me/picker_tokens/{provider_id}. Called from cmd/server/main.go
// after the handler is constructed. Passing nil leaves the subsystem
// disabled — MintPickerToken will return 503.
func (s *Server) SetPickerTokenHandler(h *PickerTokenHandler) {
	s.pickerToken = h
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
	Secret      bool   // true = mask value in API responses
	Source      string // "config" or "environment"
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
		diagramMetadata:    NewGenericMetadataHandler(GlobalMetadataRepository, "diagram", "diagram_id", nil),
		documentMetadata:   NewGenericMetadataHandler(GlobalMetadataRepository, "document", "document_id", nil),
		noteMetadata:       NewGenericMetadataHandler(GlobalMetadataRepository, "note", "note_id", nil),
		repositoryMetadata: NewGenericMetadataHandler(GlobalMetadataRepository, "repository", "repository_id", nil),
		assetMetadata:      NewGenericMetadataHandler(GlobalMetadataRepository, "asset", "asset_id", nil),
		threatMetadata:     NewGenericMetadataHandler(GlobalMetadataRepository, "threat", "threat_id", nil),
		threatModelMetadata: NewGenericMetadataHandler(GlobalMetadataRepository, "threat_model", "threat_model_id",
			func(ctx context.Context, id uuid.UUID) error {
				_, err := ThreatModelStore.Get(id.String())
				return err
			}),
		surveyMetadata: NewGenericMetadataHandler(GlobalMetadataRepository, "survey", "survey_id",
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
		surveyResponseMetadata: NewGenericMetadataHandler(GlobalMetadataRepository, "survey_response", "survey_response_id",
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
		teamMetadata:    NewGenericMetadataHandler(GlobalMetadataRepository, "team", "team_id", teamExistsFunc),
		projectMetadata: NewGenericMetadataHandler(GlobalMetadataRepository, "project", "project_id", projectExistsFunc),
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

// SetTrustedProxiesConfigured marks whether trusted proxies have been configured
func (s *Server) SetTrustedProxiesConfigured(configured bool) {
	s.trustedProxiesConfigured = configured
}

// SetRateLimitingDisabled disables all rate limiting (dev/test mode only)
func (s *Server) SetRateLimitingDisabled(disabled bool) {
	s.rateLimitingDisabled = disabled
}

// SetSettingsService sets the settings service for database-stored configuration
func (s *Server) SetSettingsService(settingsService SettingsServiceInterface) {
	s.settingsService = settingsService
}

// SetConfigProvider sets the config provider for settings migration
func (s *Server) SetConfigProvider(provider ConfigProvider) {
	s.configProvider = provider
}

// SetProviderRegistry sets the provider registry for cache invalidation from settings handlers.
func (s *Server) SetProviderRegistry(registry auth.ProviderRegistry) {
	s.providerRegistry = registry
}

// SetTicketStore sets the ticket store for WebSocket authentication
func (s *Server) SetTicketStore(ticketStore TicketStore) {
	s.ticketStore = ticketStore
}

// SetAllowHTTPWebhooks sets whether non-HTTPS webhook URLs are permitted
func (s *Server) SetAllowHTTPWebhooks(allow bool) {
	s.allowHTTPWebhooks = allow
}

// SetTimmySessionManager sets the Timmy session manager for AI assistant endpoints
func (s *Server) SetTimmySessionManager(manager *TimmySessionManager) {
	s.timmySessionManager = manager
}

// SetVectorManager sets the vector index manager for Timmy AI assistant
func (s *Server) SetVectorManager(manager *VectorIndexManager) {
	s.vectorManager = manager
}

// SetURIValidators sets the URI validators for SSRF protection.
// It also propagates validators to the sub-resource handlers.
func (s *Server) SetURIValidators(issueURI, documentURI, repositoryURI *URIValidator) {
	s.issueURIValidator = issueURI
	s.documentURIValidator = documentURI
	s.repositoryURIValidator = repositoryURI
	s.threatModelHandler.SetIssueURIValidator(issueURI)
	s.threatHandler.SetIssueURIValidator(issueURI)
	s.documentHandler.SetDocumentURIValidator(documentURI)
	s.repositoryHandler.SetRepositoryURIValidator(repositoryURI)
}

// SetContentPipeline sets the content pipeline on the document handler for
// content source detection and access validation during document creation.
// It also stores the pipeline on the Server for use by other handlers.
func (s *Server) SetContentPipeline(p *ContentPipeline) {
	s.documentHandler.SetContentPipeline(p)
	s.contentPipeline = p
}

// SetDocumentDiagnosticsDeps wires the dependencies the document GET handler
// uses to assemble per-viewer access_diagnostics. Both arguments are optional
// — when omitted, diagnostics still serialize but without linked-provider or
// service-account context.
func (s *Server) SetDocumentDiagnosticsDeps(tokens ContentTokenRepository, serviceAccountEmail string) {
	s.documentHandler.SetContentTokens(tokens)
	s.documentHandler.SetServiceAccountEmail(serviceAccountEmail)
}

// SetDocumentContentOAuthRegistry wires the content-OAuth provider registry
// onto the document handler so it can validate picker_registration payloads
// at attach time. Optional — when omitted, picker_registration is rejected
// with 422 (provider_not_registered).
func (s *Server) SetDocumentContentOAuthRegistry(r *ContentOAuthProviderRegistry) {
	s.documentHandler.SetContentOAuthRegistry(r)
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

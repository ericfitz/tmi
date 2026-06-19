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
	"github.com/ericfitz/tmi/internal/worker"
)

// SettingsServiceInterface defines the operations needed by handlers on settings.
// SEM@2ba6ca336dfda2b02702948deea087afc0b1255b: interface for reading, writing, and managing database-stored system settings
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
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: main API server holding all handlers, services, and subsystem dependencies
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
	// Async extraction pipeline (Plan 3 of #347). A nil extractionNATS means
	// the async path is unavailable and extraction falls back to inline.
	extractionNATS *worker.Conn
	extractionJobs *ExtractionJobStore
	resultConsumer *ResultConsumer
	dlqProducer    *DLQProducer
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
	timmyCore           *TimmyCore
	contentPipeline     *ContentPipeline
	// Trusted proxy configuration
	trustedProxiesConfigured bool
	// Dev-mode rate limiting bypass
	rateLimitingDisabled bool
	// credentialDeleter is used by DeleteAdminUserClientCredential. When nil the handler
	// constructs a real ClientCredentialService from the auth service; set only in tests.
	credentialDeleter credentialDeleter
	// linkedIdentityStore is used by the /me/identities/* endpoints (#383).
	// Injected from main.go or tests. When nil the handlers return 500.
	linkedIdentityStore auth.LinkedIdentityStore
	// identityLinkAuditor is used by DeleteMyIdentity to audit unlink events (#383).
	// When nil unlink events are not audited (fail-open).
	identityLinkAuditor *auth.IdentityLinkAuditor
	// systemAuditRepo is used by the admin audit query endpoints (#398).
	// Injected from main.go so the same instance used by the admin audit middleware
	// (and step-up auditor) is reused here. When nil the handlers return 500.
	systemAuditRepo SystemAuditRepository
	// contentOAuth holds the handler for the /me/content_tokens/*,
	// /admin/users/{internal_uuid}/content_tokens/*, and /oauth2/content_callback
	// endpoints. When nil, the delegated content provider subsystem is not
	// wired (e.g. no encryption key configured) and the six generated
	// interface methods short-circuit with 503.
	contentOAuth *ContentOAuthHandlers
	// pickerToken handles POST /me/picker_tokens/{provider_id}. When nil the
	// picker subsystem is not configured and the generated interface method
	// short-circuits with 503.
	pickerToken *PickerTokenHandler
	// microsoftPickerGrant handles POST /me/microsoft/picker_grants. When nil the
	// Microsoft picker-grant subsystem is not configured and the generated
	// interface method short-circuits with 503.
	microsoftPickerGrant microsoftPickerGrantHandlerInterface
	// usabilityFeedbackHandler handles GET/POST /usability_feedback endpoints.
	usabilityFeedbackHandler *UsabilityFeedbackHandler
	// contentFeedbackHandler handles GET/POST /threat_models/{id}/feedback endpoints.
	contentFeedbackHandler *ContentFeedbackHandler
	// contentSourceRegistry advertises configured content providers via the
	// /config endpoint. When nil, content_providers serializes as an empty array.
	contentSourceRegistry *ContentSourceRegistry
	// contentPickerConfigs holds browser-safe picker bootstrap values keyed by
	// content-source id. Only sources whose operator config supplies all
	// required values appear here. The /config handler emits these as
	// ContentProvider.picker_config. nil/missing entries are omitted from the
	// response.
	contentPickerConfigs map[string]map[string]string
	// contentSourceHolder is the runtime-swappable holder for the content-source
	// registry + access poller. When set, getContentSourceBundle resolves (and
	// lazily rebuilds) from it; this is the DB-backed runtime-toggle path.
	// When nil, the server uses the startup-wired contentSourceRegistry and
	// contentPipeline directly (test path or when content sources are disabled).
	contentSourceHolder *ContentSourceHolder
}

// SetContentOAuthHandlers attaches the content-OAuth handler bundle used to
// service the /me/content_tokens/*, /admin/users/{internal_uuid}/content_tokens/*,
// and /oauth2/content_callback endpoints. Called from cmd/server/main.go
// after the handler is constructed. Passing nil leaves the subsystem
// disabled — the delegation wrappers will return 503.
// SEM@74d36522781dcfd28cfc8f5f32ed5cd9dd62a25e: attach the content-OAuth handler bundle to the server, enabling token endpoints (mutates shared state)
func (s *Server) SetContentOAuthHandlers(h *ContentOAuthHandlers) {
	s.contentOAuth = h
}

// ContentOAuthHandlers returns the attached content-OAuth handler bundle
// (nil when none is wired). Exposed so callers such as the pre-user-delete
// hook wiring can register without tripping the unused-field lint.
// SEM@74d36522781dcfd28cfc8f5f32ed5cd9dd62a25e: return the attached content-OAuth handler bundle, or nil if not wired (pure)
func (s *Server) ContentOAuthHandlers() *ContentOAuthHandlers {
	return s.contentOAuth
}

// SetPickerTokenHandler attaches the picker-token handler that services
// POST /me/picker_tokens/{provider_id}. Called from cmd/server/main.go
// after the handler is constructed. Passing nil leaves the subsystem
// disabled — MintPickerToken will return 503.
// SEM@5fe247aef5f2eedfc42d4adf9058c24de12eb56e: attach the picker-token handler to the server, enabling picker mint endpoints (mutates shared state)
func (s *Server) SetPickerTokenHandler(h *PickerTokenHandler) {
	s.pickerToken = h
}

// ConfigProvider provides access to migratable settings from configuration
// SEM@600cc6a8afb0ea9ee0881874c4e9197f9d5288e7: interface for supplying migratable settings from application config
type ConfigProvider interface {
	GetMigratableSettings() []MigratableSetting
}

// MigratableSetting represents a setting that can be migrated from config to database
// SEM@33a84a2f45e6081d58584c7c6233564fb6bbf063: a typed key-value setting that can be promoted from config file to the database
type MigratableSetting struct {
	Key         string
	Value       string
	Type        string
	Description string
	Secret      bool   // true = mask value in API responses
	Source      string // "config" or "environment"
}

// NewServer creates a new API server instance
// SEM@cd6b617fb7aaaeb6491d79c87b09839f94b0fc3e: build a fully wired Server with WebSocket hub and all sub-resource handlers (mutates shared state)
func NewServer(wsLoggingConfig slogging.WebSocketLoggingConfig, inactivityTimeout time.Duration) *Server {
	wsHub := NewWebSocketHub(wsLoggingConfig, inactivityTimeout)
	return &Server{
		threatModelHandler: NewThreatModelHandler(wsHub),
		documentHandler:    NewDocumentSubResourceHandler(GlobalDocumentRepository, nil, nil, nil),
		noteHandler:        NewNoteSubResourceHandler(GlobalNoteRepository, nil, nil, nil),
		repositoryHandler:  NewRepositorySubResourceHandler(GlobalRepositoryRepository, nil, nil, nil),
		assetHandler:       NewAssetSubResourceHandler(GlobalAssetRepository, nil, nil, nil),
		threatHandler:      NewThreatSubResourceHandler(GlobalThreatRepository, nil, nil, nil),
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
		usabilityFeedbackHandler: NewUsabilityFeedbackHandler(GlobalUsabilityFeedbackRepository),
		contentFeedbackHandler:   NewContentFeedbackHandler(GlobalContentFeedbackRepository, adminDB),
	}
}

// NewServerForTests creates a server with default test configuration
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: build a Server with minimal test-safe defaults and a short inactivity timeout (pure)
func NewServerForTests() *Server {
	return NewServer(slogging.WebSocketLoggingConfig{
		Enabled:        false, // Disable logging in tests by default
		RedactTokens:   true,
		MaxMessageSize: 5 * 1024,
		OnlyDebugLevel: true,
	}, 30*time.Second) // Short timeout for tests
}

// SetAuthService sets the auth service for delegating auth-related methods
// SEM@36c1f84217ecf3f5087ad65186cd974b9b4df275: register the auth service and derive user-deletion and ownership-transfer handlers from it (mutates shared state)
func (s *Server) SetAuthService(authService AuthService) {
	s.authService = authService

	// Initialize user deletion and ownership transfer handlers with auth service
	if authAdapter, ok := authService.(*AuthServiceAdapter); ok {
		s.userDeletionHandler = NewUserDeletionHandler(authAdapter.GetService())
		s.ownershipTransferHandler = NewOwnershipTransferHandler(authAdapter.GetService())
	}
}

// SetAPIRateLimiter sets the API rate limiter
// SEM@922d880b24abd3da8955ba05fd9038f3ec43e512: register the API rate limiter on the server (mutates shared state)
func (s *Server) SetAPIRateLimiter(rateLimiter *APIRateLimiter) {
	s.apiRateLimiter = rateLimiter
}

// SetWebhookRateLimiter sets the webhook rate limiter
// SEM@922d880b24abd3da8955ba05fd9038f3ec43e512: register the webhook rate limiter on the server (mutates shared state)
func (s *Server) SetWebhookRateLimiter(rateLimiter *WebhookRateLimiter) {
	s.webhookRateLimiter = rateLimiter
}

// SetIPRateLimiter sets the IP rate limiter
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: register the IP rate limiter on the server (mutates shared state)
func (s *Server) SetIPRateLimiter(rateLimiter *IPRateLimiter) {
	s.ipRateLimiter = rateLimiter
}

// SetAuthFlowRateLimiter sets the auth flow rate limiter
// SEM@f5e41f0bdd3e5075ef62036d28d486bd0ef0286b: register the auth-flow rate limiter on the server (mutates shared state)
func (s *Server) SetAuthFlowRateLimiter(rateLimiter *AuthFlowRateLimiter) {
	s.authFlowRateLimiter = rateLimiter
}

// SetTrustedProxiesConfigured marks whether trusted proxies have been configured
// SEM@7cb03e52faae718087b1ee56a6023e9f7bddaea0: mark whether trusted proxies have been configured on the server (mutates shared state)
func (s *Server) SetTrustedProxiesConfigured(configured bool) {
	s.trustedProxiesConfigured = configured
}

// SetRateLimitingDisabled disables all rate limiting (dev/test mode only)
// SEM@c70d49ed2d6089c24d05f8bc287ba5711c73abde: disable or enable all rate limiting for dev/test mode (mutates shared state)
func (s *Server) SetRateLimitingDisabled(disabled bool) {
	s.rateLimitingDisabled = disabled
}

// SetSettingsService sets the settings service for database-stored configuration
// SEM@c937c5d55bdeac26bea04acd0677ed742a8d9eab: register the database-backed settings service on the server (mutates shared state)
func (s *Server) SetSettingsService(settingsService SettingsServiceInterface) {
	s.settingsService = settingsService
}

// SetSystemAuditRepo injects the system audit repository used by the admin
// audit query endpoints (#398). Pass the same instance used by
// NewAdminAuditMiddleware to avoid duplicate DB handles.
// SEM@7bac1ed632ff8929eff543daec4372c53d51283a: register the system audit repository for admin audit query endpoints (mutates shared state)
func (s *Server) SetSystemAuditRepo(repo SystemAuditRepository) {
	s.systemAuditRepo = repo
}

// SetLinkedIdentityStore injects the linked-identity store used by the
// /me/identities/* endpoints (#383). A nil store leaves those endpoints
// returning 500.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: register the linked-identity store for the /me/identities endpoints (mutates shared state)
func (s *Server) SetLinkedIdentityStore(store auth.LinkedIdentityStore) {
	s.linkedIdentityStore = store
}

// SetIdentityLinkAuditor injects the identity-link audit writer used to record
// unlink events from /me/identities/{id} (#383). Nil disables auditing (fail-open).
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: register the identity-link auditor to record unlink events (mutates shared state)
func (s *Server) SetIdentityLinkAuditor(a *auth.IdentityLinkAuditor) {
	s.identityLinkAuditor = a
}

// SetExtractionNATS injects the monolith's NATS connection used to publish
// extraction jobs and run the result-consumer. A nil conn disables the async path.
// SEM@a0abba4563581c2c2d54d1df58750d51e83e3e43: register the NATS connection used to publish extraction jobs (mutates shared state)
func (s *Server) SetExtractionNATS(conn *worker.Conn) { s.extractionNATS = conn }

// SetExtractionJobStore injects the extraction_jobs repository.
// SEM@a0abba4563581c2c2d54d1df58750d51e83e3e43: register the extraction job repository on the server (mutates shared state)
func (s *Server) SetExtractionJobStore(store *ExtractionJobStore) { s.extractionJobs = store }

// AsyncExtractionAvailable reports whether the async worker path can be used.
// It is false when no NATS connection is wired, which forces the inline path
// regardless of the extraction.async_enabled setting (fail-safe).
// SEM@a0abba4563581c2c2d54d1df58750d51e83e3e43: report whether a NATS connection is wired for async extraction (pure)
func (s *Server) AsyncExtractionAvailable() bool { return s.extractionNATS != nil }

// CloseExtractionNATS closes the monolith NATS connection if one is wired.
// Safe to call when no connection is set (no-op).
// SEM@a0abba4563581c2c2d54d1df58750d51e83e3e43: close the NATS connection if one is wired; no-op otherwise (mutates shared state)
func (s *Server) CloseExtractionNATS() {
	if s.extractionNATS != nil {
		s.extractionNATS.Close()
	}
}

// SetResultConsumer injects the result-consumer goroutine. The consumer must
// already have been started before calling this; the server only uses it for
// orderly shutdown via StopResultConsumer.
// SEM@28a744a1501431680450f9ab9c4d57cdf9bebd2d: register a pre-started result consumer for orderly shutdown (mutates shared state)
func (s *Server) SetResultConsumer(rc *ResultConsumer) { s.resultConsumer = rc }

// StopResultConsumer gracefully stops the result-consumer if one is wired.
// Safe to call when no consumer is set (no-op). Must be called before
// CloseExtractionNATS so the consumer can finish in-flight acks.
// SEM@28a744a1501431680450f9ab9c4d57cdf9bebd2d: gracefully stop the result consumer if one is wired; no-op otherwise (mutates shared state)
func (s *Server) StopResultConsumer() {
	if s.resultConsumer != nil {
		s.resultConsumer.Stop()
	}
}

// SetDLQProducer injects the dead-letter producer for orderly shutdown. The
// producer must already have been started.
// SEM@a8006cf44cfcde106890cf0e06d51a99145807b1: register a pre-started dead-letter queue producer for orderly shutdown (mutates shared state)
func (s *Server) SetDLQProducer(p *DLQProducer) { s.dlqProducer = p }

// StopDLQProducer gracefully stops the DLQ producer if one is wired. Safe to
// call when none is set (no-op). Call before CloseExtractionNATS.
// SEM@a8006cf44cfcde106890cf0e06d51a99145807b1: gracefully stop the DLQ producer if one is wired; no-op otherwise (mutates shared state)
func (s *Server) StopDLQProducer() {
	if s.dlqProducer != nil {
		s.dlqProducer.Stop()
	}
}

// UseAsyncExtraction reports whether extraction should route through the
// worker pipeline: the setting is on AND a NATS connection is available.
// When the setting is on but NATS is absent, it logs and returns false
// (fail-safe to inline) so extractions are never silently dropped.
// SEM@d994c2f113f9e0997f83a0815018638cc94111f7: report whether async extraction is enabled and a NATS connection is available; fails safe to inline (reads DB)
func (s *Server) UseAsyncExtraction(ctx context.Context) bool {
	if s.settingsService == nil || !s.AsyncExtractionAvailable() {
		return false
	}
	on, err := s.settingsService.GetBool(ctx, "extraction.async_enabled")
	if err != nil {
		slogging.Get().Warn("extraction.async_enabled read failed, using inline: %v", err)
		return false
	}
	return on
}

// SetConfigProvider sets the config provider for settings migration
// SEM@600cc6a8afb0ea9ee0881874c4e9197f9d5288e7: register the config provider used for settings migration (mutates shared state)
func (s *Server) SetConfigProvider(provider ConfigProvider) {
	s.configProvider = provider
}

// SetProviderRegistry sets the provider registry for cache invalidation from settings handlers.
// SEM@452dd7163303f0bb5c5b2acf7c3960183b22bb7b: register the auth provider registry for cache invalidation from settings handlers (mutates shared state)
func (s *Server) SetProviderRegistry(registry auth.ProviderRegistry) {
	s.providerRegistry = registry
}

// SetTicketStore sets the ticket store for WebSocket authentication
// SEM@e9c06824054dff110125e003301c169f002e9392: register the ticket store used for WebSocket authentication (mutates shared state)
func (s *Server) SetTicketStore(ticketStore TicketStore) {
	s.ticketStore = ticketStore
}

// SetAllowHTTPWebhooks sets whether non-HTTPS webhook URLs are permitted
// SEM@baf9ecb79a22da23c9922e1df63b14cb07d01523: configure whether non-HTTPS webhook URLs are permitted (mutates shared state)
func (s *Server) SetAllowHTTPWebhooks(allow bool) {
	s.allowHTTPWebhooks = allow
}

// SetTimmySessionManager sets the Timmy session manager for AI assistant endpoints
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: register the Timmy AI session manager on the server (mutates shared state)
func (s *Server) SetTimmySessionManager(manager *TimmySessionManager) {
	s.timmySessionManager = manager
}

// SetVectorManager sets the vector index manager for Timmy AI assistant
// SEM@773397b4fdff89166751fd8b5643ac59abce3367: register the vector index manager for the Timmy AI assistant (mutates shared state)
func (s *Server) SetVectorManager(manager *VectorIndexManager) {
	s.vectorManager = manager
}

// SetTimmyCore wires the runtime Timmy core. When set, getTimmyRuntime resolves
// the session manager from it (DB-backed, lazy rebuild) instead of the
// startup-injected timmySessionManager.
// SEM@19300f7e812ceaf4be6cadb0fe31123e70ddb707: register the Timmy core for DB-backed lazy session resolution (mutates shared state)
func (s *Server) SetTimmyCore(core *TimmyCore) {
	s.timmyCore = core
}

// getTimmyRuntime returns the live TimmyRuntime. When a TimmyCore is wired it
// resolves (and lazily rebuilds) from the database; otherwise it falls back to
// the startup-injected session manager (used by unit tests that set the manager
// directly). Returns nil when Timmy is not available; callers must nil-check.
// SEM@c309061af96f4db6e2d3a7da1d077b6a6f2f3c75: resolve the live Timmy runtime, preferring DB-backed core over startup-injected manager; returns nil when unavailable (reads DB)
func (s *Server) getTimmyRuntime(ctx context.Context) (*TimmyRuntime, error) {
	if s.timmyCore != nil {
		return s.timmyCore.Get(ctx)
	}
	if s.timmySessionManager != nil || s.vectorManager != nil {
		return &TimmyRuntime{
			SessionManager: s.timmySessionManager,
			VectorManager:  s.vectorManager,
		}, nil
	}
	return nil, nil
}

// SetURIValidators sets the URI validators for SSRF protection.
// It also propagates validators to the sub-resource handlers.
// SEM@5eacb6f5fd0d2a1861dafb4d1fc5a18f97ee8e40: register SSRF-protection URI validators and propagate them to sub-resource handlers (mutates shared state)
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
// SEM@8b1d546fd508a1785e88877c4937ea02e5f125bc: register the content pipeline on the document handler and server for source detection (mutates shared state)
func (s *Server) SetContentPipeline(p *ContentPipeline) {
	s.documentHandler.SetContentPipeline(p)
	s.contentPipeline = p
}

// SetDocumentDiagnosticsDeps wires the dependencies the document GET handler
// uses to assemble per-viewer access_diagnostics. All arguments are optional
// — when omitted, diagnostics still serialize but without linked-provider,
// service-account, or Microsoft application context.
// microsoftApplicationObjectID is the TMI Entra app's object id used to build
// the share_with_application remediation; pass "" when not configured (Task 12
// will populate it from config).
// SEM@fe4cf07a3a2b954860a8df90ba211cb0919d71de: wire per-viewer access diagnostics dependencies into the document handler (mutates shared state)
func (s *Server) SetDocumentDiagnosticsDeps(tokens ContentTokenRepository, serviceAccountEmail, microsoftApplicationObjectID string) {
	s.documentHandler.SetContentTokens(tokens)
	s.documentHandler.SetServiceAccountEmail(serviceAccountEmail)
	s.documentHandler.SetMicrosoftApplicationObjectID(microsoftApplicationObjectID)
}

// SetDocumentContentOAuthRegistry wires the content-OAuth provider registry
// onto the document handler so it can validate picker_registration payloads
// at attach time. Optional — when omitted, picker_registration is rejected
// with 422 (provider_not_registered).
// SEM@29c52159f07dd40fc350bf7cfe912f7a3a3def4b: register the content OAuth provider registry so the document handler can validate picker payloads (mutates shared state)
func (s *Server) SetDocumentContentOAuthRegistry(r *ContentOAuthProviderRegistry) {
	s.documentHandler.SetContentOAuthRegistry(r)
}

// SetDocumentAsyncExtraction wires the async extraction publisher and decider
// into the document handler. When both are non-nil and the decider returns
// true, CreateDocument returns 202 Accepted with a job_id instead of the
// usual 201. Pass nil publisher to disable the async path.
// SEM@d994c2f113f9e0997f83a0815018638cc94111f7: wire an async extraction publisher and decider into the document handler (mutates shared state)
func (s *Server) SetDocumentAsyncExtraction(publisher *ExtractionPublisher, decider func(context.Context) bool) {
	s.documentHandler.SetAsyncExtraction(publisher, decider)
}

// SetContentSourceRegistry attaches the content source registry so the
// /config handler can advertise configured providers. Mirrors the
// SetDocumentContentOAuthRegistry pattern.
// SEM@55c4ae37a85b26aa93d5a93470c1e46bd53d5e19: register the content source registry for the /config handler to advertise providers (mutates shared state)
func (s *Server) SetContentSourceRegistry(r *ContentSourceRegistry) {
	s.contentSourceRegistry = r
}

// SetContentPickerConfigs attaches browser-safe picker bootstrap values that
// the /config handler advertises as ContentProvider.picker_config for the
// matching source id. Pass nil or an empty map to clear.
// SEM@f2e01937e40c91e87ac47a34d11870fde716d093: register browser-safe picker bootstrap values advertised by the /config handler (mutates shared state)
func (s *Server) SetContentPickerConfigs(m map[string]map[string]string) {
	s.contentPickerConfigs = m
}

// SetContentSourceHolder wires the runtime content-source holder. When set,
// getContentSourceBundle resolves (and lazily rebuilds) the registry + poller
// from the holder instead of the startup-wired contentSourceRegistry field.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: register the runtime content-source holder for lazy registry and poller resolution (mutates shared state)
func (s *Server) SetContentSourceHolder(h *ContentSourceHolder) {
	s.contentSourceHolder = h
}

// getContentSourceBundle returns the live content-source bundle. When a
// ContentSourceHolder is wired it resolves (and lazily rebuilds) from the
// holder; otherwise it falls back to the startup-wired contentSourceRegistry
// and contentPipeline (used in tests or when the holder is not configured).
// Returns nil when no sources are available; callers must nil-check.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: resolve the live content-source bundle from holder or startup-wired fallback; returns nil when unavailable (pure)
func (s *Server) getContentSourceBundle(ctx context.Context) *ContentSourceBundle {
	if s.contentSourceHolder != nil {
		b, err := s.contentSourceHolder.Get(ctx)
		if err != nil {
			slogging.Get().Warn("getContentSourceBundle: holder rebuild failed: %v", err)
			return nil
		}
		return b
	}
	// Fall back to startup-wired fields (test path / no holder configured).
	if s.contentSourceRegistry != nil {
		return &ContentSourceBundle{
			Sources:  s.contentSourceRegistry,
			Pipeline: s.contentPipeline,
		}
	}
	return nil
}

// AuthService placeholder - we'll need to create this interface to avoid circular deps
// SEM@3b3ce007aac967644943c133123d85a9a1525644: interface defining auth endpoints and provider utilities the server delegates to (pure)
type AuthService interface {
	GetProviders(c *gin.Context)
	GetSAMLProviders(c *gin.Context)
	Authorize(c *gin.Context)
	StepUp(c *gin.Context)
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

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ericfitz/tmi/api" // Your module path
	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/auth" // Import auth package
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/dbcheck"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/dbschema"
	tmiotel "github.com/ericfitz/tmi/internal/otel"
	"github.com/ericfitz/tmi/internal/secrets"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gorm.io/gorm"
)

// fatalShutdownDrainTimeout bounds how long the server waits for in-flight
// requests to drain after a fatal condition is detected.
const fatalShutdownDrainTimeout = 30 * time.Second

// fatalShutdownHardExitAfter guarantees the process exits even if the graceful
// shutdown path hangs; always larger than fatalShutdownDrainTimeout.
const fatalShutdownHardExitAfter = 35 * time.Second

// Server holds dependencies for the API server
type Server struct {
	// Token blacklist for logout functionality
	tokenBlacklist *auth.TokenBlacklist
}

// HTTPSRedirectMiddleware redirects HTTP requests to HTTPS when TLS is enabled
func HTTPSRedirectMiddleware(tlsEnabled bool, tlsSubjectName string, port string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := slogging.GetContextLogger(c)

		// Only redirect if TLS is enabled and this is not already HTTPS
		// In a real environment, we'd check c.Request.TLS, but in our setup,
		// we need to rely on a header or other mechanism to determine if we're already on HTTPS
		if tlsEnabled && !isHTTPS(c.Request) {
			host := c.Request.Host

			// If we have a specific subject name, use it
			if tlsSubjectName != "" {
				if port != "443" {
					host = fmt.Sprintf("%s:%s", tlsSubjectName, port)
				} else {
					host = tlsSubjectName
				}
			}

			redirectURL := fmt.Sprintf("https://%s%s", host, c.Request.RequestURI)
			logger.Debug("Redirecting to HTTPS: %s", redirectURL)
			c.Redirect(http.StatusPermanentRedirect, redirectURL)
			c.Abort()
			return
		}
		c.Next()
	}
}

// isHTTPS determines if the request is already using HTTPS
func isHTTPS(r *http.Request) bool {
	// Check common headers set by proxies
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}

	// Check if the request was made with TLS
	if r.TLS != nil {
		return true
	}

	// Check if the request came in on the standard HTTPS port
	if r.URL.Scheme == "https" {
		return true
	}

	return false
}

// publicPaths is a map of exact paths that don't require authentication
var publicPaths = map[string]bool{
	"/":                             true,
	"/api/server-info":              true,
	"/config":                       true, // Client configuration endpoint (x-public-endpoint in OpenAPI)
	"/oauth2/callback":              true,
	"/oauth2/content_callback":      true, // Delegated-content OAuth callback (no user session yet)
	"/oauth2/providers":             true,
	"/oauth2/refresh":               true,
	"/oauth2/authorize":             true,
	"/oauth2/revoke":                true,
	"/oauth2/introspect":            true, // Token introspection per RFC 7662 (x-public-endpoint in OpenAPI)
	"/robots.txt":                   true,
	"/site.webmanifest":             true,
	"/favicon.ico":                  true,
	"/favicon.svg":                  true,
	"/web-app-manifest-192x192.png": true,
	"/web-app-manifest-512x512.png": true,
	"/TMI-Logo.svg":                 true,
	"/android-chrome-192x192.png":   true,
	"/android-chrome-512x512.png":   true,
	"/apple-touch-icon.png":         true,
	"/favicon-16x16.png":            true,
	"/favicon-32x32.png":            true,
	"/favicon-96x96.png":            true,
	// Note: /.well-known/* and /saml/* paths are handled by publicPathPrefixes below
}

// publicPathPrefixes is a list of path prefixes that don't require authentication
var publicPathPrefixes = []string{
	"/oauth2/token",
	"/static/",
	"/.well-known/",        // All OAuth/OIDC discovery endpoints
	"/saml/",               // All SAML endpoints including provider-specific routes like /saml/{provider}/login
	"/webhook-deliveries/", // Webhook delivery status endpoints (HMAC-authenticated, x-public-endpoint in OpenAPI)
}

// PublicPathsMiddleware identifies paths that don't require authentication
func PublicPathsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get a context-aware logger
		logger := slogging.GetContextLogger(c)

		// Log entry to middleware
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Processing request: %s %s", c.Request.Method, c.Request.URL.Path)
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Full URL: %s", c.Request.URL.String())
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Query params: %s", c.Request.URL.RawQuery)

		// Check if path is public (exact match or prefix match)
		isPublic := publicPaths[c.Request.URL.Path]
		if !isPublic {
			for _, prefix := range publicPathPrefixes {
				if strings.HasPrefix(c.Request.URL.Path, prefix) {
					isPublic = true
					break
				}
			}
		}

		// Note: /webhook-deliveries/ paths are public (HMAC-authenticated).
		// JWT auth for these endpoints is not supported (use /admin/webhooks/deliveries/ for JWT).

		if isPublic {
			logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] ✅ Public path identified: %s", c.Request.URL.Path)
			// Mark this request as public in the context for downstream middleware
			c.Set("isPublicPath", true)
			logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Set isPublicPath=true in context")
		} else {
			logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] ❌ Private path identified: %s", c.Request.URL.Path)
			logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] isPublicPath not set (defaults to false)")
		}

		// Log exit from middleware
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Continuing to next middleware")

		// Always continue to next middleware
		c.Next()

		// Log completion
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Middleware chain completed for: %s", c.Request.URL.Path)
	}
}

// JWT Middleware factory function that takes config, token blacklist, auth handlers, and ticket validator
func JWTMiddleware(cfg *config.Config, tokenBlacklist *auth.TokenBlacklist, authHandlers *auth.Handlers, ticketValidator *TicketValidator) gin.HandlerFunc {
	// Initialize authentication components
	publicPathChecker := &PublicPathChecker{}
	authenticator := NewJWTAuthenticator(cfg, tokenBlacklist, authHandlers, ticketValidator)

	return func(c *gin.Context) {
		logger := slogging.GetContextLogger(c)

		// Log entry to middleware
		logger.Debug("[JWT_MIDDLEWARE] *** ENTERED MIDDLEWARE FOR: %s", c.Request.URL.Path)
		logger.Debug("[JWT_MIDDLEWARE] Processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Check if this is a public path
		if publicPathChecker.IsPublicPath(c) {
			logger.Debug("[JWT_MIDDLEWARE] Continuing to next middleware (public path)")
			c.Next()
			logger.Debug("[JWT_MIDDLEWARE] Returned from middleware chain (public path)")
			return
		}

		// Perform authentication
		if err := authenticator.AuthenticateRequest(c); err != nil {
			var authErr *AuthError
			if errors.As(err, &authErr) {
				logger.Debug("[JWT_MIDDLEWARE] Authentication failed: %v", err)
				c.JSON(authErr.StatusCode, api.Error{
					Error:            authErr.Code,
					ErrorDescription: authErr.Description,
				})
				c.Abort()
				return
			}

			// Fallback for unexpected errors
			logger.Error("[JWT_MIDDLEWARE] Unexpected authentication error: %v", err)
			c.JSON(http.StatusInternalServerError, api.Error{
				Error:            "server_error",
				ErrorDescription: "Internal authentication error",
			})
			c.Abort()
			return
		}

		logger.Debug("[JWT_MIDDLEWARE] Authentication successful, proceeding to next middleware")
		c.Next()
	}
}

// Dev-mode only endpoint to get current user info
func DevUserInfoHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.GetContextLogger(c)
		logger.Debug("Handling /dev/me request")

		// Get username from context
		userID, exists := c.Get("userName")
		if !exists {
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:            "unauthorized",
				ErrorDescription: "Not authenticated",
			})
			return
		}

		userName, ok := userID.(string)
		if !ok || userName == "" {
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid user context",
			})
			return
		}

		// Get role from token if available
		role := "unknown"
		if tokenRole, exists := c.Get("userTokenRole"); exists {
			if r, ok := tokenRole.(string); ok {
				role = r
			}
		}

		// Return user info
		c.JSON(http.StatusOK, gin.H{
			"user":          userName,
			"role":          role,
			"authenticated": true,
		})
	}
}

// applyRateLimitConfig sets a custom RPM limit on an IP rate limiter if rpmOverride > 0.
func applyRateLimitConfig(limiter *api.IPRateLimiter, rpmOverride int) {
	if rpmOverride > 0 {
		limiter.DefaultLimit = rpmOverride
	}
}

// runMigrations executes Phase 2 of server startup: schema migrations, data normalization, and seeding.
// It detects DDL permission errors and checks whether the schema is already current before failing.
//
// All schema-evolution steps that mutate cross-replica state (legacy alias
// column drop, AutoMigrate's CREATE/ALTER, alias backfill, post-backfill
// unique-index creation) MUST run under a single cross-replica advisory lock.
// Acquiring the lock before AutoMigrate (rather than only around the backfill)
// closes the race where two replicas could concurrently attempt the legacy
// DROP COLUMN or duplicate-AutoMigrate operations on Oracle.
//
// SQLite (used by some narrow unit tests) does not support advisory locks; in
// that case the lock acquisition returns "unsupported dialect" and we proceed
// unlocked — a single-process in-memory SQLite is inherently single-writer.
func runMigrations(ctx context.Context, gormDB *db.GormDB, dbType string) {
	logger := slogging.Get()
	if err := runMigrationsLocked(ctx, gormDB, dbType); err != nil {
		logger.Error("%v", err)
		os.Exit(1)
	}
}

// runMigrationsLocked acquires the cross-replica schema-migration advisory
// lock and runs all schema-evolution steps under it. It returns an error
// instead of calling os.Exit so the deferred release() runs first — using
// os.Exit inside the lock-holding region would skip the deferred release
// (gocritic exitAfterDefer) and leave the lock orphaned for replicas to
// time out on.
func runMigrationsLocked(ctx context.Context, gormDB *db.GormDB, dbType string) error {
	logger := slogging.Get()

	// Wrap the entire schema-evolution sequence in one cross-replica advisory
	// lock so concurrent replicas serialize on every step (drop legacy column,
	// AutoMigrate, backfill, unique indexes), not just the backfill.
	release, lockErr := dbschema.AcquireMigrationLock(ctx, gormDB.DB(), "tmi_schema_migration")
	if lockErr != nil {
		if strings.Contains(lockErr.Error(), "unsupported dialect") {
			logger.Warn("schema migration: skipping advisory lock for dialect %q: %v", gormDB.DB().Name(), lockErr)
			release = func() {}
		} else {
			return fmt.Errorf("failed to acquire schema-migration advisory lock: %w", lockErr)
		}
	}
	defer release()

	// All databases use GORM AutoMigrate for schema management
	// This provides a single source of truth (api/models/models.go) for all supported databases
	logger.Info("==== PHASE 2: Running database migrations ====")
	logger.Info("Running GORM AutoMigrate for %s database", dbType)
	allModels := api.GetAllModels()
	if err := gormDB.AutoMigrate(allModels...); err != nil {
		errStr := err.Error()

		switch {
		case strings.Contains(errStr, "ORA-00955"):
			// Oracle: table already exists — benign, continue
			logger.Debug("Some tables already exist, continuing: %v", err)

		case dbcheck.IsPermissionError(err, dbType):
			// DDL permission denied — check if schema is already current
			logger.Warn("AutoMigrate failed with permission error: %v", err)
			sqlDB, sqlErr := gormDB.DB().DB()
			if sqlErr != nil {
				return fmt.Errorf("failed to get sql.DB for schema check: %w", sqlErr)
			}
			health, healthErr := dbcheck.CheckSchemaHealth(sqlDB, dbType)
			if healthErr != nil {
				return fmt.Errorf("failed to check schema health: %w", healthErr)
			}
			if health.IsCurrent() {
				logger.Warn("DDL permissions unavailable, but schema is up to date. Proceeding.")
			} else {
				logger.Error("Database schema requires updates but this database user lacks DDL permissions.")
				logger.Error("")
				logger.Error("Missing tables: %s", strings.Join(health.MissingTables, ", "))
				logger.Error("")
				logger.Error("To resolve this, choose one of:")
				logger.Error("  1. Run schema migration with an admin-privileged database user:")
				logger.Error("     tmi-dbtool --schema --config=<config-file>")
				logger.Error("  2. Grant DDL permissions to the current database user.")
				logger.Error("")
				logger.Error("See: https://github.com/ericfitz/tmi/wiki/Database-Security-Strategies")
				return fmt.Errorf("schema requires updates but DDL permissions are unavailable")
			}

		default:
			return fmt.Errorf("failed to auto-migrate schema: %w", err)
		}
	}
	logger.Info("GORM AutoMigrate completed for %d models", len(allModels))

	// Normalize legacy severity enum values to snake_case
	// This is idempotent: rows already lowercase are unaffected
	if result := gormDB.DB().Exec(
		"UPDATE threats SET severity = LOWER(severity) WHERE severity IS NOT NULL AND severity != LOWER(severity)",
	); result.Error != nil {
		logger.Warn("Failed to normalize severity values (non-fatal): %v", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Info("Normalized %d severity values to lowercase", result.RowsAffected)
	}
	// Migrate 'none' severity to 'informational'
	if result := gormDB.DB().Exec(
		"UPDATE threats SET severity = 'informational' WHERE severity = 'none'",
	); result.Error != nil {
		logger.Warn("Failed to migrate 'none' severity to 'informational' (non-fatal): %v", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Info("Migrated %d severity values from 'none' to 'informational'", result.RowsAffected)
	}

	// Seed required data (everyone group, webhook deny list)
	if err := seed.SeedDatabase(gormDB.DB()); err != nil {
		return fmt.Errorf("failed to seed database: %w", err)
	}

	// Backfill alias counters for all existing rows (idempotent).
	// Use the Unlocked variant because the outer schema-migration advisory
	// lock acquired at the top of this function already serializes replicas.
	if err := api.RunAliasBackfillUnlocked(ctx, gormDB.DB()); err != nil {
		return fmt.Errorf("alias backfill failed: %w", err)
	}

	// Add unique indexes for alias columns (idempotent; runs after backfill so no duplicates exist).
	if err := api.AddAliasUniqueIndexes(ctx, gormDB.DB()); err != nil {
		return fmt.Errorf("alias unique-index creation failed: %w", err)
	}

	// T19 (#356): install DB-level UPDATE/DELETE-blocking triggers on
	// audit_entries and version_snapshots so audit history cannot be
	// silently mutated even by a code path that bypasses the audit-emit
	// helper or by a hostile DB session. Idempotent across server starts.
	// Failure here is non-fatal (we log and continue) because it shouldn't
	// block the server from accepting traffic on a fresh database that
	// hasn't been granted DDL trigger permission yet — the absence of the
	// trigger is loggable but the application-level audit-emit instrumentation
	// is still in effect.
	if err := dbschema.InstallAuditAppendOnlyTriggers(ctx, gormDB.DB()); err != nil {
		logger.Warn("InstallAuditAppendOnlyTriggers failed (non-fatal; T19 protection NOT in effect): %v", err)
	}
	return nil
}

// registerStaticFiles registers static file routes on the Gin engine.
func registerStaticFiles(r *gin.Engine) {
	r.Static("/static", "./static")
	r.StaticFile("/robots.txt", "./static/robots.txt")
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	r.StaticFile("/site.webmanifest", "./static/site.webmanifest")
	r.StaticFile("/web-app-manifest-192x192.png", "./static/web-app-manifest-192x192.png")
	r.StaticFile("/web-app-manifest-512x512.png", "./static/web-app-manifest-512x512.png")
	r.StaticFile("/favicon.svg", "./static/favicon.svg")
	r.StaticFile("/TMI-Logo.svg", "./static/TMI-Logo.svg")
	r.StaticFile("/android-chrome-192x192.png", "./static/android-chrome-192x192.png")
	r.StaticFile("/android-chrome-512x512.png", "./static/android-chrome-512x512.png")
	r.StaticFile("/apple-touch-icon.png", "./static/apple-touch-icon.png")
	r.StaticFile("/favicon-16x16.png", "./static/favicon-16x16.png")
	r.StaticFile("/favicon-32x32.png", "./static/favicon-32x32.png")
	r.StaticFile("/favicon-96x96.png", "./static/favicon-96x96.png")
}

// configureTrustedProxies sets trusted proxies on the Gin engine when configured.
func configureTrustedProxies(r *gin.Engine, proxies []string) {
	if len(proxies) == 0 {
		return
	}
	if err := r.SetTrustedProxies(proxies); err != nil {
		slogging.Get().Error("Failed to set trusted proxies: %v", err)
		return
	}
	slogging.Get().Info("Trusted proxies configured: %v", proxies)
}

func setupRouter(config *config.Config) (*gin.Engine, *api.Server, *api.EmbeddingCleaner) {
	// Create a gin router without default middleware
	r := gin.New()

	// Configure gin based on log level
	if config.Logging.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Configure trusted proxies for X-Forwarded-For validation
	configureTrustedProxies(r, config.Server.TrustedProxies)

	// Add custom recovery middleware first (must be before other middleware)
	r.Use(api.CustomRecoveryMiddleware())

	// Add custom logging middleware
	r.Use(slogging.LoggerMiddleware())

	// OpenTelemetry HTTP tracing middleware
	r.Use(otelgin.Middleware("tmi"))

	// Add enhanced request/response logging middleware if configured
	if config.Logging.LogAPIRequests || config.Logging.LogAPIResponses {
		requestConfig := slogging.RequestResponseLoggingConfig{
			LogRequests:    config.Logging.LogAPIRequests,
			LogResponses:   config.Logging.LogAPIResponses,
			RedactTokens:   config.Logging.RedactAuthTokens,
			MaxBodySize:    10 * 1024, // 10KB
			OnlyDebugLevel: true,
			SkipPaths: []string{
				"/favicon.ico",
			},
		}
		r.Use(slogging.RequestResponseLogger(requestConfig))
	}

	r.Use(slogging.Recoverer())                                              // Use our recoverer
	r.Use(api.SecurityHeaders())                                             // Add security headers
	r.Use(api.CORS(config.Server.CORS.AllowedOrigins, config.Logging.IsDev)) // Handle CORS
	r.Use(api.JSONErrorHandler())                                            // Convert plain text errors to JSON
	r.Use(api.AcceptHeaderValidation())                                      // Validate Accept header (406 for unsupported types)
	r.Use(api.HSTSMiddleware(config.Server.TLSEnabled))                      // Add HSTS when TLS is enabled
	r.Use(api.ContextTimeout(30 * time.Second))

	// Enable 405 Method Not Allowed responses (RFC 9110 §15.5.6).
	// When a path is registered but the requested HTTP method is not, Gin will
	// call the NoMethod handler chain (set below after all routes are registered)
	// instead of falling through to the 404 NoRoute handler.
	// HEAD routes are explicitly registered via api.RegisterHEADRoutes (called
	// after all GET routes are registered) so that HEAD on a GET route returns
	// 200 rather than 405, as required by RFC 9110 §9.3.2.
	r.HandleMethodNotAllowed = true

	// Serve static files
	registerStaticFiles(r)

	// Security middleware with public path handling
	r.Use(PublicPathsMiddleware()) // Identify public paths first

	// Create WebSocket logging configuration from main config
	wsLoggingConfig := slogging.WebSocketLoggingConfig{
		Enabled:        config.Logging.LogWebSocketMsg,
		RedactTokens:   config.Logging.RedactAuthTokens,
		MaxMessageSize: 5 * 1024, // 5KB default
		OnlyDebugLevel: true,
	}

	// Note: API server creation moved to after store initialization
	// to ensure global stores are properly initialized first

	logger := slogging.Get()

	// ==== PHASE 1: Database Connections ====
	// Create and initialize all database connections first, before any subsystems
	logger.Info("==== PHASE 1: Initializing database connections ====")
	dbManager := db.NewManager()

	// Build GORM config from DATABASE_URL
	gormCfg := buildGormConfig(config)
	dbType := string(gormCfg.Type)
	if dbType == "" {
		logger.Error("Failed to determine database type from DATABASE_URL")
		os.Exit(1)
	}

	// Initialize GORM (required for all database types)
	logger.Info("Initializing GORM database connection for %s", dbType)
	if err := dbManager.InitGorm(gormCfg); err != nil {
		logger.Error("Failed to initialize GORM database: %v", err)
		os.Exit(1)
	}
	gormDB := dbManager.Gorm()
	if gormDB == nil {
		logger.Error("GORM database not available after initialization")
		os.Exit(1)
	}

	// Initialize Redis
	logger.Info("Initializing Redis connection")
	redisConfig := buildRedisConfig(config)
	if err := dbManager.InitRedis(redisConfig); err != nil {
		logger.Error("Failed to initialize Redis: %v", err)
		os.Exit(1)
	}

	// Test database connection via GORM
	if err := gormDB.Ping(context.Background()); err != nil {
		logger.Error("Database connection failed: %v", err)
		os.Exit(1)
	}
	logger.Info("Database connection verified successfully")

	// Set the global database manager for use throughout the application
	db.SetGlobalManager(dbManager)
	logger.Info("Global database manager set")

	// Register observable pool metrics for DB and Redis (non-fatal if unavailable)
	registerPoolMetrics(gormDB, dbManager)

	// ==== PHASE 2: Migrations ====
	runMigrations(context.Background(), gormDB, dbType)

	// ==== PHASE 3: Auth System ====
	// Initialize auth with the already-initialized database manager
	logger.Info("==== PHASE 3: Initializing authentication system ====")
	authHandlers, err := auth.InitAuthWithDB(dbManager, config)
	if err != nil {
		logger.Error("Failed to initialize authentication system: %v", err)
		os.Exit(1)
	}

	// ==== PHASE 4: API Stores ====
	logger.Info("==== PHASE 4: Initializing API stores ====")

	// Create auth service adapter for user store initialization
	var authServiceAdapter *api.AuthServiceAdapter
	if authHandlers != nil {
		authServiceAdapter = api.NewAuthServiceAdapter(authHandlers)
	}

	logger.Info("Using GORM-backed stores for %s database", dbType)
	api.InitializeGormStores(gormDB.DB(), authServiceAdapter, nil, nil)

	// Initialize administrators from configuration
	// Note: "everyone" pseudo-group and webhook deny list are seeded in PHASE 2
	if err := initializeAdministratorsGorm(config, gormDB.DB()); err != nil {
		logger.Error("Failed to initialize administrators: %v", err)
		os.Exit(1)
	}

	// Create API server with handlers (after stores are initialized)
	apiServer := api.NewServer(wsLoggingConfig, config.GetWebSocketInactivityTimeout())
	apiServer.SetAllowHTTPWebhooks(config.Webhooks.AllowHTTPTargets)

	// Initialize settings service for database-stored configuration
	logger.Info("Initializing settings service for database-stored configuration")
	settingsService := api.NewSettingsService(gormDB.DB(), dbManager.Redis())
	apiServer.SetSettingsService(settingsService)

	// Initialize settings encryption (must be before SeedDefaults so defaults are encrypted)
	secretsProvider, err := secrets.NewProvider(context.Background(), &config.Secrets)
	if err != nil {
		logger.Warn("Failed to initialize secrets provider for settings encryption: %v", err)
	} else {
		encryptor, err := crypto.NewSettingsEncryptor(context.Background(), secretsProvider)
		if err != nil {
			logger.Error("Failed to initialize settings encryptor: %v", err)
		} else {
			settingsService.SetEncryptor(encryptor)
			// Also inject into Redis for transparent encryption of sensitive cached values
			if dbManager.Redis() != nil {
				dbManager.Redis().SetEncryptor(encryptor)
				logger.Info("Redis value encryption enabled for sensitive keys")
			}
		}
		if closeErr := secretsProvider.Close(); closeErr != nil {
			logger.Warn("Failed to close secrets provider: %v", closeErr)
		}
	}

	// Seed default settings (non-blocking - continue even if seeding fails)
	if err := settingsService.SeedDefaults(context.Background()); err != nil {
		logger.Warn("Failed to seed default settings: %v", err)
	} else {
		logger.Info("Default system settings seeded successfully")
	}

	// Set config provider for settings migration endpoint and priority lookups
	// Configuration priority: environment > config file > database
	// The config provider (environment/config file values) takes precedence over database values
	configProvider := api.NewConfigProviderAdapter(config)
	apiServer.SetConfigProvider(configProvider)
	settingsService.SetConfigProvider(configProvider)
	logger.Info("Config provider set for settings migration and priority lookups")

	// Create provider registry for unified auth provider lookup
	providerSettingsReader := api.NewProviderSettingsReaderAdapter(settingsService)
	authConfigForRegistry := auth.ConfigFromUnified(config)
	providerRegistry := auth.NewDefaultProviderRegistry(
		authConfigForRegistry.OAuth.Providers,
		authConfigForRegistry.SAML.Providers,
		providerSettingsReader,
	)
	apiServer.SetProviderRegistry(providerRegistry)
	logger.Info("Provider registry initialized for lazy-loading database providers")

	// Setup server with handlers
	server := &Server{}

	// Set up auth service adapter for OpenAPI integration (reuse the one created during store initialization)
	if authServiceAdapter != nil {
		apiServer.SetAuthService(authServiceAdapter)
		logger.Info("Auth service adapter configured for OpenAPI integration")

		// Set up global auth service for event owner lookups
		api.SetGlobalAuthServiceForEvents(authServiceAdapter)
		logger.Info("Global auth service configured for webhook event owner UUID lookups")

		// Set up the addon-invocation delegation-token issuer (T18, #358).
		// The webhook delivery worker mints a per-attempt scoped JWT for each
		// addon.invoked delivery so the addon can perform write-backs as the
		// invoker rather than as its own service-account.
		api.SetGlobalDelegationTokenIssuer(authServiceAdapter)
		logger.Info("Global delegation-token issuer configured (T18 / #358 addon write-back hardening)")

		// Set up admin checker adapter for /me endpoint using Administrators group
		adminChecker := api.NewGroupBasedAdminChecker(gormDB.DB(), api.GlobalGroupMemberRepository)
		authHandlers.SetAdminChecker(adminChecker)
		logger.Info("Admin checker adapter configured for auth handlers (Administrators group)")

		// Set up claims enricher for JWT token generation (admin + security reviewer claims)
		claimsEnricher := api.NewGroupMembershipEnricher(api.GlobalGroupMemberRepository, gormDB.DB())
		authHandlers.Service().SetClaimsEnricher(claimsEnricher)
		logger.Info("Claims enricher configured for JWT token generation")

		// Set up user groups fetcher for /me endpoint
		userGroupsFetcher := api.NewGormUserGroupsFetcher(api.GlobalGroupMemberRepository)
		authHandlers.SetUserGroupsFetcher(userGroupsFetcher)
		logger.Info("User groups fetcher configured for /me endpoint")

		// Configure HttpOnly session cookies
		authHandlers.SetCookieOptions(auth.CookieOptions{
			Domain:     config.GetCookieDomain(),
			Secure:     config.IsSecureCookies(),
			Enabled:    config.Auth.Cookie.Enabled,
			ExpiresIn:  config.Auth.JWT.ExpirationSeconds,
			RefreshTTL: config.Auth.JWT.RefreshTokenDays * 86400,
		})
		logger.Info("HttpOnly session cookies configured (enabled=%t, secure=%t, domain=%s)",
			config.Auth.Cookie.Enabled, config.IsSecureCookies(), config.GetCookieDomain())

		// Wire provider registry into auth handlers for unified provider lookup
		authHandlers.SetProviderRegistry(providerRegistry)
		authHandlers.Service().SetProviderRegistry(providerRegistry)
		logger.Info("Provider registry wired into auth handlers for unified provider lookup")
	} else {
		logger.Warn("Auth handlers not available - auth endpoints will return errors")
	}

	// ==== PHASE 5: Redis Services ====
	// Initialize Redis-backed services (token blacklist, rate limiters, event emitter)
	logger.Info("==== PHASE 5: Initializing Redis services ====")
	var ticketStore api.TicketStore
	if dbManager != nil && dbManager.Redis() != nil {
		logger.Info("Initializing token blacklist service")
		server.tokenBlacklist = auth.NewTokenBlacklist(dbManager.Redis().GetClient(), authHandlers.Service().GetKeyManager())

		// Initialize event emitter for webhook support
		logger.Info("Initializing event emitter for webhook subscriptions")
		api.InitializeEventEmitter(dbManager.Redis().GetClient(), "tmi:events")

		// Initialize unified webhook delivery store
		logger.Info("Initializing webhook delivery store")
		api.GlobalWebhookDeliveryRedisStore = api.NewWebhookDeliveryRedisStore(dbManager.Redis())

		// Initialize rate limiters
		logger.Info("Initializing API rate limiter")
		apiServer.SetAPIRateLimiter(api.NewAPIRateLimiter(dbManager.Redis().GetClient(), api.GlobalUserAPIQuotaStore))

		logger.Info("Initializing webhook rate limiter")
		apiServer.SetWebhookRateLimiter(api.NewWebhookRateLimiter(dbManager.Redis().GetClient()))

		if config.Server.DisableRateLimiting {
			logger.Warn("Rate limiting is DISABLED via configuration (disable_rate_limiting=true)")
			apiServer.SetRateLimitingDisabled(true)
		}

		logger.Info("Initializing IP rate limiter")
		ipLimiter := api.NewIPRateLimiter(dbManager.Redis().GetClient())
		applyRateLimitConfig(ipLimiter, config.Server.RateLimitPublicRPM)
		apiServer.SetIPRateLimiter(ipLimiter)

		logger.Info("Initializing auth flow rate limiter")
		apiServer.SetAuthFlowRateLimiter(api.NewAuthFlowRateLimiter(dbManager.Redis().GetClient()))

		// Initialize addon rate limiter
		logger.Info("Initializing addon rate limiter")
		api.GlobalAddonRateLimiter = api.NewAddonRateLimiter(dbManager.Redis(), api.GlobalAddonInvocationQuotaStore)

		// Initialize quota cache for dynamic adjustment (60 second TTL)
		logger.Info("Initializing quota cache with 60 second TTL")
		api.InitializeQuotaCache(60 * time.Second)

		// Initialize ticket store for WebSocket authentication
		logger.Info("Initializing WebSocket ticket store (Redis-backed)")
		ticketStore = api.NewRedisTicketStore(dbManager.Redis())
	} else {
		logger.Warn("Redis not available - token blacklist service disabled")
		logger.Warn("Redis not available - event emitter disabled (webhooks will not emit events)")
		logger.Warn("Redis not available - rate limiting disabled")
		logger.Warn("Redis not available - quota caching disabled")

		// Initialize in-memory ticket store fallback
		logger.Warn("Redis not available, using in-memory ticket store (not suitable for multi-instance deployments)")
		ticketStore = api.NewInMemoryTicketStore()
	}
	apiServer.SetTicketStore(ticketStore)
	apiServer.SetTrustedProxiesConfigured(len(config.Server.TrustedProxies) > 0)

	// ==== Content OAuth handlers (must come BEFORE Timmy init so the token
	// repo and OAuth registry are available for the GoogleWorkspace source,
	// picker handler, and access poller's LinkedProviderChecker) ====
	contentTokenRepo, contentOAuthRegistry := wireContentOAuthHandlers(apiServer, config, gormDB.DB(), dbManager, authHandlers)

	// ==== PHASE 6: Timmy AI Assistant ====
	initializeTimmySubsystem(config, apiServer, contentTokenRepo, contentOAuthRegistry)

	// Start embedding idle cleanup (runs unconditionally, even if Timmy is disabled,
	// to clean up embeddings if Timmy was previously enabled)
	var embeddingCleaner *api.EmbeddingCleaner
	if config.Timmy.EmbeddingCleanupIntervalMinutes > 0 {
		cleanupInterval := time.Duration(config.Timmy.EmbeddingCleanupIntervalMinutes) * time.Minute
		embeddingCleaner = api.NewEmbeddingCleaner(
			api.GlobalTimmyEmbeddingStore,
			gormDB.DB(),
			cleanupInterval,
			config.Timmy.EmbeddingIdleDaysActive,
			config.Timmy.EmbeddingIdleDaysClosed,
		)
		embeddingCleaner.Start()
	}

	// Initialize access tracker for last_accessed_at updates
	api.GlobalAccessTracker = api.NewAccessTracker(gormDB.DB())

	// Add comprehensive request tracing middleware first
	r.Use(api.DetailedRequestLoggingMiddleware())
	r.Use(api.RouteMatchingMiddleware())

	// Test debug logging is working
	logger.Debug("[MAIN] Testing debug logging - this should appear in logs!")

	// Add IP-based rate limiting middleware first (for public endpoints)
	r.Use(api.IPRateLimitMiddleware(apiServer))

	// Add auth flow rate limiting middleware (for OAuth/SAML endpoints)
	r.Use(api.AuthFlowRateLimitMiddleware(apiServer))

	// Add input validation middleware (BEFORE JWT auth to return proper 4XX codes for malformed requests)
	// This follows RFC 9110 guidance: validate request structure before checking authentication
	r.Use(api.MethodNotAllowedHandler())              // Validate HTTP methods (405 for invalid methods)
	r.Use(api.PathParameterValidationMiddleware())    // Validate path parameters for security
	r.Use(api.UUIDValidationMiddleware())             // Validate UUID format in path parameters
	r.Use(api.DuplicateHeaderValidationMiddleware())  // Reject duplicate critical security headers (RFC 7230)
	r.Use(api.TransferEncodingValidationMiddleware()) // Reject Transfer-Encoding headers (unsupported)
	r.Use(api.ContentTypeValidationMiddleware())      // Validate Content-Type (415 for unsupported)
	r.Use(api.AcceptLanguageMiddleware())             // Handle Accept-Language gracefully
	r.Use(api.UnicodeNormalizationMiddleware())       // Normalize and reject problematic Unicode (security hardening)
	r.Use(api.StrictJSONValidationMiddleware())       // Reject malformed JSON (trailing garbage, duplicate keys)
	r.Use(api.BoundaryValueValidationMiddleware())    // Enhanced validation for boundary values

	// Now add JWT middleware with token blacklist support, auth handlers, and ticket validator
	// This runs AFTER basic validation so malformed requests get 4XX, not 401
	ticketValidator := NewTicketValidator(ticketStore, authHandlers)
	r.Use(JWTMiddleware(config, server.tokenBlacklist, authHandlers, ticketValidator)) // JWT auth with public path skipping

	// Enrich OTel spans with TMI-specific attributes (user ID, resource IDs)
	r.Use(api.OTelSpanEnrichmentMiddleware())

	// Add user-based rate limiting middleware (after JWT so internal_uuid is available)
	r.Use(api.RateLimitMiddleware(apiServer))

	// Add server middleware to make API server available in context
	r.Use(serverContextMiddleware(apiServer))

	// Add middleware to provide server configuration to handlers
	// This must be before routes are registered so config is available to all endpoints
	r.Use(serverConfigMiddleware(config))

	// Normalize enum values to canonical snake_case before OpenAPI validation
	r.Use(api.EnumNormalizerMiddleware())

	// Convert HEAD requests to GET before OpenAPI validation (RFC 9110 Section 9.3.2)
	// This must be after auth/rate-limiting (which handle HEAD correctly) and before
	// the OpenAPI validator (which would reject HEAD as an unknown method)
	r.Use(api.HeadMethodMiddleware())

	// Add OpenAPI validation middleware
	if openAPIValidator, err := api.SetupOpenAPIValidation(); err != nil {
		logger.Error("Failed to setup OpenAPI validation middleware: %v", err)
		os.Exit(1)
	} else {
		r.Use(openAPIValidator)
	}

	// Unified declarative authorization (issue #341). For each annotated
	// operation enforces the gates in x-tmi-authz; for unannotated paths
	// passes through to legacy per-resource middleware below.
	r.Use(api.AuthzMiddleware())

	// Apply entity-specific middleware
	r.Use(api.ThreatModelMiddleware())
	r.Use(api.DiagramMiddleware())

	// Apply Timmy feature gate middleware
	r.Use(api.TimmyEnabledMiddleware(config.Timmy))
	logger.Info("Timmy middleware configured (enabled=%v, configured=%v)", config.Timmy.Enabled, config.Timmy.IsConfigured())

	// Apply automation group membership middleware for /automation/* routes
	r.Use(api.AutomationMiddleware())
	r.Use(api.EmbeddingAutomationMiddleware())
	logger.Info("Automation middleware configured for /automation/* paths")

	// Register WebSocket and custom non-REST routes
	logger.Info("Registering WebSocket and custom routes")
	apiServer.RegisterHandlers(r)

	// Validate database schema after auth initialization
	logger.Info("Validating database schema...")
	if err := validateDatabaseSchema(config); err != nil {
		logger.Error("Database schema validation failed: %v", err)
		// In production, you might want to exit here
		// os.Exit(1)
	}

	// Register API routes except for auth routes which are handled by the auth package
	// Register OpenAPI-generated routes with the API server instance
	logger.Info("[MAIN_MODULE] Starting OpenAPI route registration")
	logger.Info("[MAIN_MODULE] Registering OpenAPI route: GET /auth/me -> GetCurrentUser")
	logger.Info("[MAIN_MODULE] Registering OpenAPI route: GET /auth/providers -> GetAuthProviders")
	logger.Info("[MAIN_MODULE] Registering OpenAPI route: GET /me/sessions -> GetCollaborationSessions")

	// Use RegisterHandlersWithOptions to provide custom error handler for parameter binding errors
	// This ensures all validation errors return JSON responses per OpenAPI spec
	api.RegisterHandlersWithOptions(r, apiServer, api.GinServerOptions{
		ErrorHandler: api.GinServerErrorHandler,
	})
	logger.Info("[MAIN_MODULE] OpenAPI route registration completed (includes admin endpoints with AdministratorMiddleware)")

	// Add development routes when in dev mode
	if config.Logging.IsDev {
		logger := slogging.Get()
		logger.Info("Adding development-only endpoints")
		r.GET("/dev/me", DevUserInfoHandler()) // Endpoint to check current user
	}

	// Register HEAD routes for every GET route (except excluded protocol paths).
	// This must be done AFTER all GET routes are registered so that r.Routes()
	// returns a complete list.  The HeadMethodMiddleware (already registered via
	// r.Use) converts HEAD→GET inside the handler, suppresses the body, and sets
	// Content-Length; the underlying GET handler logic runs unchanged.
	api.RegisterHEADRoutes(r)
	logger.Info("HEAD routes registered for all eligible GET endpoints")

	// Set the NoMethod handler that returns JSON 405 for truly unsupported methods.
	// This runs after HandleMethodNotAllowed=true causes Gin to detect a method
	// mismatch (path exists but method not registered).  Gin pre-populates the
	// Allow header before calling these handlers.
	r.NoMethod(api.MethodNotAllowedJSONHandler())
	logger.Info("NoMethod handler configured (returns JSON 405)")

	return r, apiServer, embeddingCleaner
}

// wireContentOAuthHandlers constructs the delegated content provider handler
// bundle and attaches it to the shared *api.Server. Routes for the
// /me/content_tokens/*, /admin/users/{internal_uuid}/content_tokens/*, and
// /oauth2/content_callback endpoints are registered by the generated
// RegisterHandlersWithOptions call — this function only wires the dependency
// the generated interface methods delegate to.
//
// When no encryption key is configured (common until an operator enables a
// content provider), or when any required collaborator is unavailable, the
// function logs and leaves the handler unset; the generated delegation
// wrappers will respond with 503 for the affected endpoints.
func wireContentOAuthHandlers(apiServer *api.Server, cfg *config.Config, gormDB *gorm.DB, dbManager *db.Manager, authHandlers *auth.Handlers) (api.ContentTokenRepository, *api.ContentOAuthProviderRegistry) {
	logger := slogging.Get()

	if cfg.ContentTokenEncryptionKey == "" {
		logger.Info("delegated content providers disabled; /me/content_tokens endpoints will return 503")
		return nil, nil
	}

	enc, err := api.NewContentTokenEncryptor(cfg.ContentTokenEncryptionKey)
	if err != nil {
		logger.Error("failed to create content token encryptor: %v — /me/content_tokens endpoints will return 503", err)
		return nil, nil
	}

	tokenRepo := api.NewGormContentTokenRepository(gormDB, enc)

	registry, err := api.LoadContentOAuthRegistryFromConfig(cfg.ContentOAuth)
	if err != nil {
		logger.Error("failed to load content OAuth registry: %v — /me/content_tokens endpoints will return 503", err)
		return nil, nil
	}

	if dbManager.Redis() == nil {
		logger.Error("Redis is not available — /me/content_tokens endpoints will return 503 (Redis required for OAuth state)")
		return nil, nil
	}
	stateStore := api.NewContentOAuthStateStore(dbManager.Redis().GetClient())

	allowList := api.NewClientCallbackAllowList(cfg.ContentOAuth.AllowedClientCallbacks)

	// userLookup extracts the caller's internal UUID from the JWT middleware
	// context. "userInternalUUID" is set by jwt_auth.go after successful
	// token validation.
	userLookup := func(c *gin.Context) (string, bool) {
		v, ok := c.Get("userInternalUUID")
		if !ok {
			return "", false
		}
		s, ok := v.(string)
		return s, ok && s != ""
	}

	h := &api.ContentOAuthHandlers{
		Cfg:           cfg.ContentOAuth,
		Registry:      registry,
		StateStore:    stateStore,
		Tokens:        tokenRepo,
		CallbackAllow: allowList,
		UserLookup:    userLookup,
	}

	apiServer.SetContentOAuthHandlers(h)

	// Wire registry onto the document handler so CreateDocument can validate
	// picker_registration payloads. Per-handler diagnostic deps (tokens +
	// serviceAccountEmail) are wired in initializeTimmySubsystem alongside
	// the contentSources / accessPoller setup.
	apiServer.SetDocumentContentOAuthRegistry(registry)

	// Wire the document store onto ContentOAuthHandlers so the un-link
	// cascade (DELETE /me/content_tokens/{provider_id}) can clear picker
	// columns on the user's documents.
	h.Documents = api.GlobalDocumentRepository

	// Construct the picker-token handler and attach it. The configs map
	// populates from cfg.ContentSources.GoogleWorkspace and
	// cfg.ContentSources.Microsoft; additional delegated providers can be
	// added here as they're implemented.
	pickerConfigs := map[string]api.PickerTokenConfig{}
	if cfg.ContentSources.GoogleWorkspace.IsConfigured() {
		pickerConfigs[api.ProviderGoogleWorkspace] = api.PickerTokenConfig{
			DeveloperKey: cfg.ContentSources.GoogleWorkspace.PickerDeveloperKey,
			AppID:        cfg.ContentSources.GoogleWorkspace.PickerAppID,
			ProviderConfig: map[string]string{
				"developer_key": cfg.ContentSources.GoogleWorkspace.PickerDeveloperKey,
				"app_id":        cfg.ContentSources.GoogleWorkspace.PickerAppID,
			},
		}
	}
	if cfg.ContentSources.Microsoft.Enabled {
		if !cfg.ContentSources.Microsoft.IsConfigured() {
			logger.Error("content_sources.microsoft.enabled=true requires tenant_id, client_id, and application_object_id; refusing to start")
			os.Exit(1)
		}
		pickerConfigs[api.ProviderMicrosoft] = api.PickerTokenConfig{
			ProviderConfig: map[string]string{
				"client_id":     cfg.ContentSources.Microsoft.ClientID,
				"tenant_id":     cfg.ContentSources.Microsoft.TenantID,
				"picker_origin": cfg.ContentSources.Microsoft.PickerOrigin,
			},
		}
	}
	pickerHandler := api.NewPickerTokenHandler(tokenRepo, registry, pickerConfigs, userLookup)
	apiServer.SetPickerTokenHandler(pickerHandler)
	if len(pickerConfigs) == 0 {
		logger.Info("picker-token handler attached but no providers configured; all requests will return 422")
	} else {
		logger.Info("picker-token handler attached (configured providers: %d)", len(pickerConfigs))
	}

	// Wire the Microsoft picker-grant handler when Microsoft is configured.
	// Note: Enabled+IsConfigured validation already ran in the picker-token block above.
	if cfg.ContentSources.Microsoft.Enabled {
		msGrantHandler := api.NewMicrosoftPickerGrantHandler(
			tokenRepo,
			registry,
			cfg.ContentSources.Microsoft.ApplicationObjectID,
			"", // graphBaseURL: empty → defaults to https://graph.microsoft.com/v1.0
			userLookup,
		)
		apiServer.SetMicrosoftPickerGrantHandler(msGrantHandler)
		logger.Info("Microsoft picker-grant handler attached")
	}

	// Register a pre-user-delete hook so that content-token revocations are
	// swept at the provider side before the FK cascade removes the rows.
	// Best-effort — failures are logged but never block user deletion.
	if authHandlers != nil {
		authHandlers.Service().SetPreUserDeleteHook(h)
		logger.Info("content-token pre-delete revocation hook registered")
	}

	logger.Info("delegated content OAuth handler wired (providers: %v)", registry.IDs())
	return tokenRepo, registry
}

// initializeTimmySubsystem sets up the Timmy AI assistant when configured.
// contentTokenRepo and contentOAuthRegistry are provided by wireContentOAuthHandlers
// (called before this function) so the GoogleWorkspace delegated source, picker-token
// handler, and access poller can be wired with provider-aware dispatch.
// NOTE: All content-source plumbing (including GoogleWorkspace) is gated on
// cfg.Timmy.Enabled — a pre-existing architectural constraint. Enabling Google
// Workspace also requires Timmy to be enabled.
func initializeTimmySubsystem(cfg *config.Config, apiServer *api.Server, contentTokenRepo api.ContentTokenRepository, contentOAuthRegistry *api.ContentOAuthProviderRegistry) {
	logger := slogging.Get()

	if !cfg.Timmy.Enabled {
		return
	}

	if !cfg.Timmy.IsConfigured() {
		logger.Warn("Timmy is enabled but LLM/embedding providers are not configured — Timmy endpoints will return 503")
		return
	}

	logger.Info("==== PHASE 6: Initializing Timmy AI assistant ====")

	vectorManager := api.NewVectorIndexManager(
		api.GlobalTimmyEmbeddingStore,
		cfg.Timmy.MaxMemoryMB,
		cfg.Timmy.InactivityTimeoutSeconds,
	)
	apiServer.SetVectorManager(vectorManager)

	registry := api.NewContentProviderRegistry()
	registry.Register(api.NewDirectTextProvider())
	registry.Register(api.NewJSONContentProvider())
	// Build URI validators from SSRF config
	issueURIValidator := buildURIValidator(cfg.SSRF.IssueURI, "TMI_SSRF_ISSUE_URI")
	documentURIValidator := buildURIValidator(cfg.SSRF.DocumentURI, "TMI_SSRF_DOCUMENT_URI")
	repositoryURIValidator := buildURIValidator(cfg.SSRF.RepositoryURI, "TMI_SSRF_REPOSITORY_URI")
	timmyURIValidator := buildURIValidator(cfg.SSRF.Timmy, "TMI_SSRF_TIMMY")

	apiServer.SetURIValidators(issueURIValidator, documentURIValidator, repositoryURIValidator)

	// Build two-layer content pipeline for URI-based content
	contentSources := api.NewContentSourceRegistry()

	// Register Google Workspace delegated source if configured. Must register
	// BEFORE GoogleDriveSource so FindSourceForDocument can pick the delegated
	// source for picker-attached docs (URL patterns overlap with google_drive).
	//
	// Startup validation: GoogleWorkspace.Enabled implies the delegated content
	// OAuth infrastructure must be available (encryption key set, registry
	// loaded, google_workspace OAuth provider enabled). Otherwise the feature
	// can never function — exit cleanly so the operator notices in dev.
	if cfg.ContentSources.GoogleWorkspace.Enabled {
		if contentTokenRepo == nil || contentOAuthRegistry == nil {
			logger.Error("content_sources.google_workspace.enabled=true requires content-token encryption key and OAuth provider configuration; refusing to start")
			os.Exit(1)
		}
		if _, ok := contentOAuthRegistry.Get(api.ProviderGoogleWorkspace); !ok {
			logger.Error("content_sources.google_workspace.enabled=true requires content_oauth.providers.google_workspace.enabled=true; refusing to start")
			os.Exit(1)
		}
		if !cfg.ContentSources.GoogleWorkspace.IsConfigured() {
			logger.Error("content_sources.google_workspace.enabled=true requires picker_developer_key and picker_app_id; refusing to start")
			os.Exit(1)
		}
		gwSource := api.NewDelegatedGoogleWorkspaceSource(
			contentTokenRepo,
			contentOAuthRegistry,
			cfg.ContentSources.GoogleWorkspace.PickerDeveloperKey,
			cfg.ContentSources.GoogleWorkspace.PickerAppID,
		)
		contentSources.Register(gwSource)
		logger.Info("Content source enabled: google_workspace (delegated, drive.file scope)")
	}

	// Register Confluence delegated source if configured. Like Google Workspace,
	// this must be registered before HTTPSource (which matches all http/https
	// URLs). Confluence has no picker UX, so URL-pattern dispatch alone routes
	// correctly: CanHandle returns true only for *.atlassian.net hosts under
	// /wiki/, leaving the URL pattern matcher to mark them as "confluence".
	if cfg.ContentSources.Confluence.Enabled {
		if contentTokenRepo == nil || contentOAuthRegistry == nil {
			logger.Error("content_sources.confluence.enabled=true requires content-token encryption key and OAuth provider configuration; refusing to start")
			os.Exit(1)
		}
		confluenceProvider, ok := contentOAuthRegistry.Get(api.ProviderConfluence)
		if !ok {
			logger.Error("content_sources.confluence.enabled=true requires content_oauth.providers.confluence.enabled=true; refusing to start")
			os.Exit(1)
		}
		// Warn (non-fatal) when offline_access is missing — refresh tokens
		// will not be issued and users will need to re-link after each
		// access-token expiry.
		hasOfflineAccess := false
		for _, scope := range confluenceProvider.RequiredScopes() {
			if scope == "offline_access" {
				hasOfflineAccess = true
				break
			}
		}
		if !hasOfflineAccess {
			logger.Warn("content_oauth.providers.confluence.required_scopes does not include 'offline_access'; refresh tokens will not be issued and users will need to re-link after access tokens expire")
		}
		confluenceSource := api.NewDelegatedConfluenceSource(contentTokenRepo, contentOAuthRegistry)
		contentSources.Register(confluenceSource)
		logger.Info("Content source enabled: confluence (delegated)")
	}

	// Register Microsoft delegated source when configured. Must register
	// BEFORE HTTPSource (which matches all http/https URLs) since SharePoint
	// URLs (*.sharepoint.com) would otherwise match the HTTP fallback.
	if cfg.ContentSources.Microsoft.Enabled {
		if contentTokenRepo == nil || contentOAuthRegistry == nil {
			logger.Error("content_sources.microsoft.enabled=true requires content-token encryption key and OAuth provider configuration; refusing to start")
			os.Exit(1)
		}
		if !cfg.ContentSources.Microsoft.IsConfigured() {
			logger.Error("content_sources.microsoft.enabled=true requires tenant_id, client_id, and application_object_id; refusing to start")
			os.Exit(1)
		}
		msProvider, ok := contentOAuthRegistry.Get(api.ProviderMicrosoft)
		if !ok {
			logger.Error("content_sources.microsoft.enabled=true requires content_oauth.providers.microsoft.enabled=true; refusing to start")
			os.Exit(1)
		}
		hasOfflineAccess := false
		for _, scope := range msProvider.RequiredScopes() {
			if scope == "offline_access" {
				hasOfflineAccess = true
				break
			}
		}
		if !hasOfflineAccess {
			logger.Warn("content_oauth.providers.microsoft.required_scopes does not include 'offline_access'; users will need to re-link after access tokens expire")
		}
		msSource := api.NewDelegatedMicrosoftSource(contentTokenRepo, contentOAuthRegistry)
		contentSources.Register(msSource)
		logger.Info("Content source enabled: microsoft (delegated, OneDrive-for-Business + SharePoint)")
	}

	// Register Google Drive source if configured (must be before HTTPSource, which matches all http/https URLs)
	if cfg.ContentSources.GoogleDrive.IsConfigured() {
		gdSource, gdErr := api.NewGoogleDriveSource(
			cfg.ContentSources.GoogleDrive.CredentialsFile,
			cfg.ContentSources.GoogleDrive.ServiceAccountEmail,
		)
		if gdErr != nil {
			logger.Error("Failed to initialize Google Drive source: %v", gdErr)
		} else {
			contentSources.Register(gdSource)
			logger.Info("Content source enabled: google_drive (service account: %s)",
				cfg.ContentSources.GoogleDrive.ServiceAccountEmail)
		}
	}

	contentSources.Register(api.NewHTTPSource(timmyURIValidator))

	pipeline := buildContentPipeline(cfg, contentSources, logger)
	logger.Info("Content sources enabled: %s", strings.Join(contentSources.Names(), ", "))

	// Adapter: pipeline implements ContentProvider for URI-based refs
	registry.Register(api.NewPipelineContentProvider(pipeline))

	// Wire pipeline into document handler for content source detection on creation
	apiServer.SetContentPipeline(pipeline)

	// Wire diagnostics deps onto the document handler (Task 8.2b).
	// When contentTokenRepo is nil (delegated providers disabled), diagnostics
	// still serialize but with empty linked-provider context.
	// serviceAccountEmail is empty when google_drive is unconfigured; the
	// share_with_service_account remediation degrades gracefully.
	apiServer.SetDocumentDiagnosticsDeps(
		contentTokenRepo,
		cfg.ContentSources.GoogleDrive.ServiceAccountEmail,
		cfg.ContentSources.Microsoft.ApplicationObjectID,
	)

	// Start background access poller for pending document access
	accessPoller := api.NewAccessPoller(
		contentSources,
		api.GlobalDocumentRepository,
		5*time.Minute,
		7*24*time.Hour,
	)
	// Inject picker-aware dispatch (Task 8.2d). Must be set before Start
	// per SetLinkedProviderChecker's lifecycle contract.
	if contentTokenRepo != nil {
		accessPoller.SetLinkedProviderChecker(api.NewContentTokenLinkedChecker(contentTokenRepo))
	}
	accessPoller.Start()

	rateLimiter := api.NewTimmyRateLimiter(
		cfg.Timmy.MaxMessagesPerUserPerHour,
		cfg.Timmy.MaxSessionsPerThreatModel,
		cfg.Timmy.MaxConcurrentLLMRequests,
	)

	llmService, llmErr := api.NewTimmyLLMService(cfg.Timmy)
	if llmErr != nil {
		logger.Error("Failed to initialize Timmy LLM service: %v", llmErr)
		return
	}

	// Create reranker if configured
	var reranker api.Reranker
	if cfg.Timmy.IsRerankConfigured() {
		rerankHTTPClient := &http.Client{
			Timeout: time.Duration(cfg.Timmy.LLMTimeoutSeconds) * time.Second,
		}
		reranker = api.NewAPIReranker(
			cfg.Timmy.RerankBaseURL, cfg.Timmy.RerankModel,
			cfg.Timmy.RerankAPIKey, cfg.Timmy.RerankTopK, rerankHTTPClient,
		)
		logger.Info("Timmy reranker configured (model=%s)", cfg.Timmy.RerankModel)
	}

	// Create query decomposer if enabled
	var decomposer api.QueryDecomposer
	if cfg.Timmy.QueryDecompositionEnabled && llmService != nil {
		decomposer = api.NewLLMQueryDecomposer(llmService)
		logger.Info("Timmy query decomposition enabled")
	}

	sessionManager := api.NewTimmySessionManager(
		cfg.Timmy, llmService, vectorManager, registry, rateLimiter,
		reranker, decomposer,
	)
	apiServer.SetTimmySessionManager(sessionManager)
	logger.Info("Timmy AI assistant initialized (provider=%s, model=%s)", cfg.Timmy.LLMProvider, cfg.Timmy.LLMModel)
}

// buildContentPipeline assembles the OOXML-aware content pipeline: validates
// the operator-tunable extractor limits, registers all extractors (plain text,
// HTML, PDF, DOCX, PPTX, XLSX), wires the per-user concurrency limiter that
// reads `users.extraction_concurrency_override`, and applies the wall-clock
// budget. The lookup closure is cached per-user for the process lifetime by
// the limiter (override changes don't resize an existing semaphore — known
// limitation, see design spec).
func buildContentPipeline(cfg *config.Config, contentSources *api.ContentSourceRegistry, logger *slogging.Logger) *api.ContentPipeline {
	// Validate content_extractors config (the top-level Config.Validate
	// already runs this — second call here is for fail-fast clarity at the
	// wiring point, with a clear error message if values were mutated
	// post-load).
	if err := cfg.ContentExtractors.Validate(); err != nil {
		logger.Error("Invalid content_extractors config: %v", err)
		os.Exit(1)
	}

	ooxmlExtractorLimits := api.OOXMLLimitsFromConfig(cfg.ContentExtractors)

	contentExtractors := api.NewContentExtractorRegistry()
	contentExtractors.Register(api.NewPlainTextExtractor())
	contentExtractors.Register(api.NewHTMLExtractor())
	contentExtractors.Register(api.NewPDFExtractor())
	contentExtractors.Register(api.NewDOCXExtractor(ooxmlExtractorLimits))
	contentExtractors.Register(api.NewPPTXExtractor(ooxmlExtractorLimits))
	contentExtractors.Register(api.NewXLSXExtractor(ooxmlExtractorLimits))

	usersTable := (&models.User{}).TableName()
	limiter := api.NewConcurrencyLimiter(
		cfg.ContentExtractors.PerUserConcurrencyDefault,
		func(ctx context.Context, userID string) (int, error) {
			mgr := db.GetGlobalManager()
			if mgr == nil || mgr.Gorm() == nil {
				return 0, fmt.Errorf("database not initialized")
			}
			var override sql.NullInt64
			row := mgr.Gorm().DB().WithContext(ctx).Raw(
				"SELECT extraction_concurrency_override FROM "+usersTable+" WHERE internal_uuid = ?", userID,
			).Row()
			if err := row.Scan(&override); err != nil {
				return 0, err
			}
			if !override.Valid {
				return 0, nil
			}
			return int(override.Int64), nil
		},
	)

	pipeline := api.NewContentPipelineWithLimiter(
		contentSources, contentExtractors, api.NewURLPatternMatcher(),
		limiter,
		api.PipelineLimits{WallClockBudget: cfg.ContentExtractors.WallClockBudget},
	)

	logger.Info("OOXML extractors enabled: docx, pptx, xlsx (per-user concurrency default: %d, wall-clock budget: %s)",
		cfg.ContentExtractors.PerUserConcurrencyDefault,
		cfg.ContentExtractors.WallClockBudget,
	)

	return pipeline
}

// buildURIValidator creates a URIValidator from SSRF config with environment variable overrides.
func buildURIValidator(cfg config.SSRFURIConfig, envPrefix string) *api.URIValidator {
	allowlistStr := cfg.Allowlist
	if envVal := os.Getenv(envPrefix + "_ALLOWLIST"); envVal != "" {
		allowlistStr = envVal
	}
	schemesStr := cfg.Schemes
	if envVal := os.Getenv(envPrefix + "_SCHEMES"); envVal != "" {
		schemesStr = envVal
	}

	var allowlist []string
	if allowlistStr != "" {
		for _, entry := range strings.Split(allowlistStr, ",") {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				allowlist = append(allowlist, entry)
			}
		}
	}

	var schemes []string
	if schemesStr != "" {
		for _, s := range strings.Split(schemesStr, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				schemes = append(schemes, s)
			}
		}
	}

	return api.NewURIValidator(allowlist, schemes)
}

// serverContextMiddleware makes the API server available in the request context.
func serverContextMiddleware(apiServer *api.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("server", apiServer)
		c.Next()
	}
}

// serverConfigMiddleware provides server configuration values to all handlers via context.
func serverConfigMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("tlsEnabled", cfg.Server.TLSEnabled)
		c.Set("tlsSubjectName", cfg.Server.TLSSubjectName)
		c.Set("serverPort", cfg.Server.Port)
		c.Set("isDev", cfg.Logging.IsDev)
		c.Set("operatorName", cfg.Operator.Name)
		c.Set("operatorContact", cfg.Operator.Contact)
		c.Next()
	}
}

// initCloudLogging initializes cloud logging based on environment variables.
// Returns a CloudLogWriter if enabled and configured, nil otherwise.
func initCloudLogging() (slogging.CloudLogWriter, *slogging.LogLevel) {
	if os.Getenv("TMI_CLOUD_LOG_ENABLED") != "true" {
		return nil, nil
	}

	provider := os.Getenv("TMI_CLOUD_LOG_PROVIDER")
	if provider != "oci" {
		slogging.Get().Warn("TMI_CLOUD_LOG_ENABLED=true but TMI_CLOUD_LOG_PROVIDER=%s is not supported", provider)
		return nil, nil
	}

	logID := os.Getenv("TMI_OCI_LOG_ID")
	if logID == "" {
		slogging.Get().Warn("TMI_CLOUD_LOG_ENABLED=true but TMI_OCI_LOG_ID not set")
		return nil, nil
	}

	ociWriter, err := slogging.NewOCICloudWriter(context.Background(), slogging.OCICloudWriterConfig{
		LogID:        logID,
		Source:       "tmi-server",
		BatchSize:    100,
		FlushTimeout: 5 * time.Second,
	})
	if err != nil {
		slogging.Get().Error("Failed to create OCI cloud writer: %v (continuing without cloud logging)", err)
		return nil, nil
	}

	slogging.Get().Info("OCI cloud logging enabled, log ID: %s", logID)

	// Parse cloud log level if specified
	var cloudLogLevel *slogging.LogLevel
	if cloudLevelStr := os.Getenv("TMI_CLOUD_LOG_LEVEL"); cloudLevelStr != "" {
		level := slogging.ParseLogLevel(cloudLevelStr)
		cloudLogLevel = &level
	}

	return ociWriter, cloudLogLevel
}

// startWebhookWorkers initializes and starts all webhook workers
func startWebhookWorkers(ctx context.Context, cfg *config.Config) (*api.WebhookEventConsumer, *api.WebhookChallengeWorker, *api.WebhookDeliveryWorker, *api.WebhookCleanupWorker) {
	logger := slogging.Get()

	// Start webhook workers if database and Redis are available
	dbManager := db.GetGlobalManager()
	var webhookConsumer *api.WebhookEventConsumer
	var challengeWorker *api.WebhookChallengeWorker
	var deliveryWorker *api.WebhookDeliveryWorker
	var cleanupWorker *api.WebhookCleanupWorker

	if dbManager != nil && dbManager.Gorm() != nil {
		logger.Info("Starting webhook workers...")

		// Build SSRF validator for outbound webhook calls. The schemes default
		// to https; if cfg.Webhooks.AllowHTTPTargets is set, http is also
		// allowed. Allowlist comes from cfg.SSRF.Webhook (env override
		// TMI_SSRF_WEBHOOK_ALLOWLIST). With no allowlist, the validator
		// applies the SSRF blocklist (private/loopback/link-local/metadata).
		webhookSSRFCfg := cfg.SSRF.Webhook
		if cfg.Webhooks.AllowHTTPTargets && webhookSSRFCfg.Schemes == "" {
			webhookSSRFCfg.Schemes = "http,https"
		}
		webhookURIValidator := buildURIValidator(webhookSSRFCfg, "TMI_SSRF_WEBHOOK")

		// Start event consumer (requires Redis)
		if dbManager.Redis() != nil {
			webhookConsumer = api.NewWebhookEventConsumer(
				dbManager.Redis().GetClient(),
				"tmi:events",
				"webhook-consumers",
				fmt.Sprintf("consumer-%d", time.Now().Unix()),
			)
			if err := webhookConsumer.Start(ctx); err != nil {
				logger.Error("Failed to start webhook event consumer: %v", err)
			}
		} else {
			logger.Warn("Redis not available, webhook event consumer disabled")
		}

		// Start challenge verification worker
		challengeWorker = api.NewWebhookChallengeWorker(webhookURIValidator)
		if err := challengeWorker.Start(ctx); err != nil {
			logger.Error("Failed to start webhook challenge worker: %v", err)
		}

		// Start delivery worker
		deliveryWorker = api.NewWebhookDeliveryWorker(webhookURIValidator)
		if err := deliveryWorker.Start(ctx); err != nil {
			logger.Error("Failed to start webhook delivery worker: %v", err)
		}

		// Start cleanup worker
		cleanupWorker = api.NewWebhookCleanupWorker()
		if err := cleanupWorker.Start(ctx); err != nil {
			logger.Error("Failed to start webhook cleanup worker: %v", err)
		}

		logger.Info("Webhook workers started successfully")
	} else {
		logger.Warn("Database not available, webhook workers disabled")
	}

	return webhookConsumer, challengeWorker, deliveryWorker, cleanupWorker
}

func main() {
	// Parse command line flags
	configFile, generateConfig, err := config.ParseFlags()
	if err != nil {
		slogging.Get().Error("Error parsing flags: %v", err)
		os.Exit(1)
	}

	// Generate example config files if requested
	if generateConfig {
		if err := config.GenerateExampleConfig(); err != nil {
			slogging.Get().Error("Error generating config: %v", err)
			os.Exit(1)
		}
		return
	}

	// Load configuration
	cfg, err := config.Load(configFile)
	if err != nil {
		slogging.Get().Error("Error loading configuration: %v", err)
		os.Exit(1)
	}

	// Initialize cloud logging if enabled via environment variables
	cloudWriter, cloudLogLevel := initCloudLogging()

	// Initialize logger
	if err := slogging.Initialize(slogging.Config{
		Level:                       cfg.GetLogLevel(),
		IsDev:                       cfg.Logging.IsDev,
		LogDir:                      cfg.Logging.LogDir,
		MaxAgeDays:                  cfg.Logging.MaxAgeDays,
		MaxSizeMB:                   cfg.Logging.MaxSizeMB,
		MaxBackups:                  cfg.Logging.MaxBackups,
		AlsoLogToConsole:            cfg.Logging.AlsoLogToConsole,
		CloudErrorThreshold:         cfg.Logging.CloudErrorThreshold,
		SuppressUnauthenticatedLogs: cfg.Logging.SuppressUnauthenticatedLogs,
		CloudWriter:                 cloudWriter,
		CloudLogLevel:               cloudLogLevel,
	}); err != nil {
		slogging.Get().Error("Failed to initialize logger: %v", err)
		os.Exit(1)
	}

	// Run the server; os.Exit is deferred to allow defers in runServer to execute
	os.Exit(runServer(cfg))
}

// initOTel initializes OpenTelemetry and registers all TMI metric instruments.
// Returns the OTel shutdown function on success.
func initOTel(ctx context.Context, cfg *config.Config) (func(context.Context) error, error) {
	otelCfg := tmiotel.Config{
		Enabled:        cfg.Observability.Enabled,
		SamplingRate:   cfg.Observability.SamplingRate,
		PrometheusPort: cfg.Observability.PrometheusPort,
	}
	otelShutdown, err := tmiotel.Setup(ctx, otelCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize OpenTelemetry: %w", err)
	}

	tmiMetrics, err := tmiotel.NewTMIMetrics()
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel metrics: %w", err)
	}
	tmiotel.GlobalMetrics = tmiMetrics

	return otelShutdown, nil
}

// registerPoolMetrics wires up observable gauge metrics for the DB and Redis connection pools.
// Failures are non-fatal and logged as warnings.
func registerPoolMetrics(gormDB *db.GormDB, dbManager *db.Manager) {
	logger := slogging.Get()
	sqlDB, err := gormDB.DB().DB()
	if err != nil {
		logger.Warn("Failed to get underlying sql.DB for pool metrics: %v", err)
		return
	}
	dbStatsFn := func() tmiotel.DBPoolStats {
		s := sqlDB.Stats()
		return tmiotel.DBPoolStats{
			OpenConnections: s.OpenConnections,
			Idle:            s.Idle,
			InUse:           s.InUse,
			WaitCount:       s.WaitCount,
			WaitDuration:    s.WaitDuration,
		}
	}
	var redisStatsFn func() tmiotel.RedisPoolStats
	if dbManager.Redis() != nil {
		redisClient := dbManager.Redis().GetClient()
		redisStatsFn = func() tmiotel.RedisPoolStats {
			ps := redisClient.PoolStats()
			return tmiotel.RedisPoolStats{
				ActiveCount: int(ps.TotalConns - ps.IdleConns),
				IdleCount:   int(ps.IdleConns),
			}
		}
	}
	if err := tmiotel.RegisterPoolMetrics(dbStatsFn, redisStatsFn); err != nil {
		logger.Warn("Failed to register pool metrics: %v", err)
	} else {
		logger.Info("DB and Redis pool metrics registered")
	}
}

// runServer runs the TMI API server and returns an exit code.
// This function is separate from main() so that deferred cleanup (logger close, signal restore)
// executes before os.Exit is called in main().
func runServer(cfg *config.Config) int {
	// Get logger instance
	logger := slogging.Get()
	defer func() {
		if err := logger.Close(); err != nil {
			slogging.Get().Error("Error closing logger: %v", err)
		}
	}()

	// Log startup information
	logger.Info("Starting TMI API server")
	logger.Info("Environment: %s", map[bool]string{true: "development", false: "production"}[cfg.Logging.IsDev])
	logger.Info("Log level: %s", cfg.Logging.Level)

	// Create a context that will be canceled on shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Install fatal handler so dberrors.HandleFatal triggers graceful shutdown
	// instead of calling os.Exit directly. A backup timer guarantees the process
	// exits even if graceful shutdown hangs.
	fatalShutdownCh := make(chan error, 1)
	dberrors.SetFatalHandler(func(err error) {
		select {
		case fatalShutdownCh <- err:
		default:
		}
		go func() {
			time.Sleep(fatalShutdownHardExitAfter)
			slogging.Get().Error("Fatal shutdown deadline expired, forcing exit")
			os.Exit(1)
		}()
	})

	// Initialize OpenTelemetry and register metric instruments
	otelShutdown, err := initOTel(ctx, cfg)
	if err != nil {
		logger.Error("Failed to initialize OpenTelemetry: %v", err)
		return 1
	}

	// Setup router with config
	r, apiServer, embeddingCleaner := setupRouter(cfg)

	// Add HTTPS redirect middleware if enabled
	if cfg.Server.TLSEnabled && cfg.Server.HTTPToHTTPSRedirect {
		r.Use(HTTPSRedirectMiddleware(
			cfg.Server.TLSEnabled,
			cfg.Server.TLSSubjectName,
			cfg.Server.Port,
		))
	}

	// Start WebSocket hub with context for cleanup
	apiServer.StartWebSocketHub(ctx)

	// Start audit pruner for background cleanup of old audit entries and version snapshots
	apiServer.StartAuditPruner()

	// Initialize and start webhook workers
	webhookConsumer, challengeWorker, deliveryWorker, cleanupWorker := startWebhookWorkers(ctx, cfg)

	// Prepare address
	addr := fmt.Sprintf("%s:%s", cfg.Server.Interface, cfg.Server.Port)

	// Validate TLS configuration if enabled
	if cfg.Server.TLSEnabled {
		if cfg.Server.TLSCertFile == "" || cfg.Server.TLSKeyFile == "" {
			logger.Error("TLS enabled but certificate or key file not specified")
			return 1
		}

		// Check that files exist
		if _, err := os.Stat(cfg.Server.TLSCertFile); os.IsNotExist(err) {
			logger.Error("TLS certificate file not found: %s", cfg.Server.TLSCertFile)
			return 1
		}

		if _, err := os.Stat(cfg.Server.TLSKeyFile); os.IsNotExist(err) {
			logger.Error("TLS key file not found: %s", cfg.Server.TLSKeyFile)
			return 1
		}

		// Load certificate to verify it's valid
		cert, err := tls.LoadX509KeyPair(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		if err != nil {
			logger.Error("Failed to load TLS certificate and key: %s", err)
			return 1
		}

		// Try to parse the first certificate to get more information
		if len(cert.Certificate) > 0 {
			x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				logger.Warn("Failed to parse X509 certificate: %s", err)
			} else {
				logger.Info("TLS certificate subject: %s", x509Cert.Subject.CommonName)
				logger.Info("TLS certificate expires: %s", x509Cert.NotAfter.Format(time.RFC3339))

				// Warn if subject name doesn't match
				if x509Cert.Subject.CommonName != cfg.Server.TLSSubjectName {
					logger.Warn("Certificate subject name (%s) doesn't match configured TLS_SUBJECT_NAME (%s)",
						x509Cert.Subject.CommonName, cfg.Server.TLSSubjectName)
				}

				// Check certificate expiration
				if x509Cert.NotAfter.Before(time.Now().AddDate(0, 1, 0)) {
					if x509Cert.NotAfter.Before(time.Now()) {
						logger.Error("TLS certificate has expired on %s",
							x509Cert.NotAfter.Format(time.RFC3339))
					} else {
						logger.Warn("TLS certificate will expire within 1 month on %s",
							x509Cert.NotAfter.Format(time.RFC3339))
					}
				}
			}
		}
	}

	// Configure TLS if enabled
	var tlsConfig *tls.Config
	if cfg.Server.TLSEnabled {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: cfg.Server.TLSSubjectName,
		}
	}

	// Configure server with timeouts from config
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
		TLSConfig:    tlsConfig,
	}

	// Channel to capture server startup errors
	serverErrCh := make(chan error, 1)

	// Start server in a goroutine
	go func() {
		var err error

		if cfg.Server.TLSEnabled {
			logger.Info("Server listening on %s with TLS enabled", addr)
			logger.Info("Using certificate: %s, key: %s, subject name: %s",
				cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile, cfg.Server.TLSSubjectName)
			err = srv.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		} else {
			logger.Info("Server listening on %s", addr)
			err = srv.ListenAndServe()
		}

		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Error starting server: %s", err)
			serverErrCh <- err
		}
	}()

	// Wait for interrupt signal, server error, or fatal shutdown trigger
	var fatalTriggered bool
	select {
	case <-ctx.Done():
		// Normal shutdown path
	case err := <-serverErrCh:
		logger.Error("Server failed to start: %v", err)
		return 1
	case err := <-fatalShutdownCh:
		logger.Error("Fatal shutdown triggered: %v", err)
		fatalTriggered = true
	}

	// Restore default behavior on the interrupt signal and notify user of shutdown
	stop()
	logger.Info("Shutting down server...")

	// Fatal shutdowns get a longer drain window so legitimate in-flight requests
	// can finish before the backup hard-exit timer fires.
	drainTimeout := 10 * time.Second
	if fatalTriggered {
		drainTimeout = fatalShutdownDrainTimeout
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	// Gracefully shutdown the server
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server forced to shutdown: %s", err)
		return 1
	}

	logger.Info("Server gracefully stopped")

	stopBackgroundWorkers(apiServer, webhookConsumer, challengeWorker, deliveryWorker, cleanupWorker, embeddingCleaner)

	// Shutdown OpenTelemetry (flush pending spans/metrics)
	logger.Info("Shutting down OpenTelemetry...")
	if err := otelShutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down OpenTelemetry: %v", err)
	}

	return 0
}

// stopBackgroundWorkers gracefully stops all background workers during server shutdown.
func stopBackgroundWorkers(
	apiServer *api.Server,
	webhookConsumer *api.WebhookEventConsumer,
	challengeWorker *api.WebhookChallengeWorker,
	deliveryWorker *api.WebhookDeliveryWorker,
	cleanupWorker *api.WebhookCleanupWorker,
	embeddingCleaner *api.EmbeddingCleaner,
) {
	logger := slogging.Get()

	// Stop webhook workers
	if webhookConsumer != nil {
		logger.Info("Stopping webhook event consumer...")
		webhookConsumer.Stop()
	}
	if challengeWorker != nil {
		logger.Info("Stopping webhook challenge worker...")
		challengeWorker.Stop()
	}
	if deliveryWorker != nil {
		logger.Info("Stopping webhook delivery worker...")
		deliveryWorker.Stop()
	}
	if cleanupWorker != nil {
		logger.Info("Stopping webhook cleanup worker...")
		cleanupWorker.Stop()
	}

	// Stop audit pruner and flush debouncer
	logger.Info("Stopping audit pruner...")
	apiServer.StopAuditPruner()
	if api.GlobalAuditDebouncer != nil {
		logger.Info("Flushing audit debouncer...")
		api.GlobalAuditDebouncer.FlushAll()
	}

	// Stop embedding cleaner
	if embeddingCleaner != nil {
		logger.Info("Stopping embedding cleaner...")
		embeddingCleaner.Stop()
	}

	// Shutdown auth system
	if err := auth.Shutdown(context.TODO()); err != nil {
		logger.Error("Error shutting down auth system: %v", err)
	}
}

// validateDatabaseSchema validates the database schema matches expectations
func validateDatabaseSchema(cfg *config.Config) error {
	// Only validate for PostgreSQL databases (schema validation uses PostgreSQL-specific queries)
	if !strings.HasPrefix(cfg.Database.URL, "postgres://") && !strings.HasPrefix(cfg.Database.URL, "postgresql://") {
		slogging.Get().Debug("Skipping schema validation for non-PostgreSQL database")
		return nil
	}

	// Use DATABASE_URL directly
	connStr := cfg.Database.URL

	// Open database connection
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			// Log error but don't fail validation
			slogging.Get().Error("Error closing database: %v", err)
		}
	}()

	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Validate schema
	result, err := dbschema.ValidateSchema(db)
	if err != nil {
		return fmt.Errorf("failed to validate schema: %w", err)
	}

	// Validation results are already logged by the validator

	// Check if validation failed
	if !result.Valid {
		return fmt.Errorf("schema validation failed: %d errors, %d/%d tables found",
			len(result.Errors), result.AppliedMigrations, result.TotalMigrations)
	}

	return nil
}

// initializeAdministratorsGorm initializes administrators from configuration using GORM
func initializeAdministratorsGorm(cfg *config.Config, gormDB *gorm.DB) error {
	logger := slogging.Get()
	logger.Info("Initializing administrators from configuration (GORM)")

	if len(cfg.Administrators) == 0 {
		logger.Warn("No administrators configured - add at least one admin to manage the system")
		return nil
	}

	ctx := context.Background()
	adminsGroupUUID := uuid.MustParse(api.AdministratorsGroupUUID)
	notes := "Configured via YAML/environment"

	for i, adminCfg := range cfg.Administrators {
		logger.Debug("Processing administrator config[%d]: provider=%s, type=%s", i, adminCfg.Provider, adminCfg.SubjectType)

		if adminCfg.SubjectType == "user" {
			// Look up user by provider + (provider_id OR email)
			userUUID, err := findUserByProviderIdentityGorm(ctx, gormDB, adminCfg.Provider, adminCfg.ProviderId, adminCfg.Email)
			if err != nil {
				// User doesn't exist - attempt to create if we have required fields
				if adminCfg.Email == "" {
					logger.Warn("Cannot create user for admin config[%d]: email is required but not provided", i)
					continue
				}

				// Refuse to create a placeholder row when provider_id is empty.
				// A row with provider_user_id="" is keyed differently than the row
				// the OAuth login flow will produce (which has the real
				// provider_user_id from the IdP). The OAuth login is supposed to
				// reconcile via the Tier-2 (provider+email) match in
				// findOrCreateUserWithResolver, but that has produced orphan
				// duplicates in the past. Refusing the create at the source is
				// the simplest defense: operators must either (a) supply
				// provider_id in admin config, or (b) wait for the user to log in
				// once and then add to Administrators via the /admin/users API.
				// Either path leaves the database with a single, well-keyed row.
				if adminCfg.ProviderId == "" {
					logger.Warn(
						"Skipping admin config[%d] for %s: provider_id is empty. Add provider_id to the config OR have the user log in first, then add them to the Administrators group via /admin/users. Skipping to avoid orphan-prone seed row.",
						i, adminCfg.Email)
					continue
				}

				// Create the user with available information
				createdUser, createErr := createUserForAdministratorGorm(ctx, gormDB, adminCfg)
				if createErr != nil {
					logger.Error("Failed to create user for admin config[%d]: provider=%s, provider_id=%s, email=%s, error=%v",
						i, adminCfg.Provider, adminCfg.ProviderId, adminCfg.Email, createErr)
					continue
				}

				logger.Info("Created user for configured administrator: provider=%s, provider_id=%s, email=%s, internal_uuid=%s",
					adminCfg.Provider, adminCfg.ProviderId, adminCfg.Email, createdUser)
				userUUID = createdUser
			}

			// Add user to Administrators group
			_, err = api.GlobalGroupMemberRepository.AddMember(ctx, adminsGroupUUID, userUUID, nil, &notes)
			if err != nil {
				logger.Info("Administrator user already in group or added: provider=%s, error=%v", adminCfg.Provider, err)
			} else {
				logger.Info("Administrator configured: type=user, provider=%s, user_uuid=%s", adminCfg.Provider, userUUID)
			}
		} else if adminCfg.SubjectType == "group" {
			// Look up group by provider + group_name
			groupUUID, err := findGroupByProviderAndNameGorm(ctx, gormDB, adminCfg.Provider, adminCfg.GroupName)
			if err != nil {
				logger.Warn("Could not find group for admin config[%d]: provider=%s, group_name=%s, error=%v",
					i, adminCfg.Provider, adminCfg.GroupName, err)
				// Group doesn't exist yet - it will be created when first referenced
				continue
			}

			// Add group to Administrators group (group-in-group membership)
			_, err = api.GlobalGroupMemberRepository.AddGroupMember(ctx, adminsGroupUUID, groupUUID, nil, &notes)
			if err != nil {
				logger.Info("Administrator group already in group or added: provider=%s, error=%v", adminCfg.Provider, err)
			} else {
				logger.Info("Administrator configured: type=group, provider=%s, group_uuid=%s", adminCfg.Provider, groupUUID)
			}
		}
	}

	logger.Info("Administrator initialization complete")
	return nil
}

// findUserByProviderIdentityGorm looks up a user by provider and provider_id or email using GORM
func findUserByProviderIdentityGorm(ctx context.Context, gormDB *gorm.DB, provider string, providerID string, email string) (uuid.UUID, error) {
	var user struct {
		InternalUUID string `gorm:"column:internal_uuid"`
	}

	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	query := gormDB.WithContext(ctx).Table("users").
		Select("internal_uuid").
		Where(map[string]any{"provider": provider})

	switch {
	case providerID != "":
		query = query.Where(map[string]any{"provider_user_id": providerID})
	case email != "":
		query = query.Where(map[string]any{"email": email})
	default:
		return uuid.Nil, fmt.Errorf("either provider_id or email is required")
	}

	if err := query.First(&user).Error; err != nil {
		return uuid.Nil, err
	}

	return uuid.Parse(user.InternalUUID)
}

// createUserForAdministratorGorm creates a new user record for a configured administrator using GORM
func createUserForAdministratorGorm(ctx context.Context, gormDB *gorm.DB, adminCfg config.AdministratorConfig) (uuid.UUID, error) {
	logger := slogging.Get()

	// Generate internal UUID for the new user
	internalUUID := uuid.New()

	// Derive a name from the email if not provided
	name := adminCfg.Email
	if idx := strings.Index(name, "@"); idx > 0 {
		name = name[:idx] // Use email prefix as name
	}

	// Create user using GORM
	user := map[string]any{
		"internal_uuid":    internalUUID.String(),
		"provider":         adminCfg.Provider,
		"provider_user_id": adminCfg.ProviderId,
		"email":            adminCfg.Email,
		"name":             name,
		"email_verified":   false,
		"created_at":       time.Now(),
		"modified_at":      time.Now(),
	}

	if err := gormDB.WithContext(ctx).Table("users").Create(user).Error; err != nil {
		logger.Error("Failed to insert user for administrator: provider=%s, email=%s, error=%v",
			adminCfg.Provider, adminCfg.Email, err)
		return uuid.Nil, fmt.Errorf("failed to create user: %w", err)
	}

	return internalUUID, nil
}

// findGroupByProviderAndNameGorm looks up a group by provider and group_name using GORM
func findGroupByProviderAndNameGorm(ctx context.Context, gormDB *gorm.DB, provider string, groupName string) (uuid.UUID, error) {
	var group models.Group

	// Use struct-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	if err := gormDB.WithContext(ctx).
		Where(&models.Group{Provider: provider, GroupName: groupName}).
		First(&group).Error; err != nil {
		return uuid.Nil, err
	}

	return uuid.Parse(group.InternalUUID)
}

// buildGormConfig creates a GORM configuration from the application config.
// DATABASE_URL is required and contains all connection parameters.
func buildGormConfig(cfg *config.Config) db.GormConfig {
	log := slogging.Get()

	// DATABASE_URL is required (validated in config.Validate)
	log.Info("Using TMI_DATABASE_URL for database configuration")
	parsedCfg, err := db.ParseDatabaseURL(cfg.Database.URL)
	if err != nil {
		log.Error("Failed to parse TMI_DATABASE_URL: %v", err)
		// Return empty config - will fail on connection
		return db.GormConfig{}
	}

	// Copy connection pool settings from config
	parsedCfg.MaxOpenConns = cfg.Database.ConnectionPool.MaxOpenConns
	parsedCfg.MaxIdleConns = cfg.Database.ConnectionPool.MaxIdleConns
	parsedCfg.ConnMaxLifetime = cfg.Database.ConnectionPool.ConnMaxLifetime
	parsedCfg.ConnMaxIdleTime = cfg.Database.ConnectionPool.ConnMaxIdleTime

	// Copy Oracle wallet location if set (cannot be encoded in URL)
	if cfg.Database.OracleWalletLocation != "" {
		parsedCfg.OracleWalletLocation = cfg.Database.OracleWalletLocation
	}

	return *parsedCfg
}

// buildRedisConfig creates a Redis configuration from the application config.
// If TMI_REDIS_URL is set, it takes precedence over individual fields.
func buildRedisConfig(cfg *config.Config) db.RedisConfig {
	log := slogging.Get()

	// If REDIS_URL is provided, parse it and use those values
	if cfg.Database.Redis.URL != "" {
		log.Info("Using TMI_REDIS_URL for Redis configuration")
		host, port, password, dbNum, err := db.ParseRedisURL(cfg.Database.Redis.URL)
		if err != nil {
			log.Error("Failed to parse TMI_REDIS_URL: %v, falling back to individual fields", err)
		} else {
			return db.RedisConfig{
				Host:     host,
				Port:     port,
				Password: password,
				DB:       dbNum,
			}
		}
	}

	// Fall back to individual fields
	return db.RedisConfig{
		Host:     cfg.Database.Redis.Host,
		Port:     cfg.Database.Redis.Port,
		Password: cfg.Database.Redis.Password,
		DB:       cfg.Database.Redis.DB,
	}
}

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/auth/repository"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// stepUpStubUserRepo is a UserRepository stub that supports GetByProviderID lookup.
// Distinct from stubUserRepo in client_credentials_grant_test.go which always
// returns ErrUserNotFound for that method.
type stepUpStubUserRepo struct {
	byProviderID map[string]*repository.User // key: "<provider>|<providerUserID>"
	byID         map[string]*repository.User // key: InternalUUID
}

func (r *stepUpStubUserRepo) GetByID(_ context.Context, id string) (*repository.User, error) {
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}
func (r *stepUpStubUserRepo) GetByEmail(context.Context, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *stepUpStubUserRepo) GetByProviderID(_ context.Context, provider, providerUserID string) (*repository.User, error) {
	if u, ok := r.byProviderID[provider+"|"+providerUserID]; ok {
		return u, nil
	}
	return nil, repository.ErrUserNotFound
}
func (r *stepUpStubUserRepo) GetByProviderAndEmail(context.Context, string, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *stepUpStubUserRepo) GetByAnyProviderID(context.Context, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}
func (r *stepUpStubUserRepo) GetProviders(context.Context, string) ([]repository.UserProvider, error) {
	return nil, nil
}
func (r *stepUpStubUserRepo) GetPrimaryProviderID(context.Context, string) (string, error) {
	return "", repository.ErrUserNotFound
}
func (r *stepUpStubUserRepo) Create(context.Context, *repository.User) (*repository.User, error) {
	return nil, nil
}
func (r *stepUpStubUserRepo) Update(context.Context, *repository.User) error { return nil }
func (r *stepUpStubUserRepo) Delete(context.Context, string) error           { return nil }

// stepUpTestHarness bundles the constructed Handlers + the test JWT + the in-memory audit writer.
type stepUpTestHarness struct {
	handlers     *Handlers
	testJWT      string
	auditW       *memorySystemAuditWriter
	mr           *miniredis.Miniredis
	cleanup      func()
	originalUser repository.User
}

// stepUpHarnessOpt configures the step-up test harness.
type stepUpHarnessOpt func(*stepUpHarnessConfig)

type stepUpHarnessConfig struct {
	// Providers to register. If empty, defaults to google (strong).
	providers map[string]OAuthProviderConfig
	// JWT identity: claim.Subject. If empty, defaults to uid-alice.
	jwtSubject string
	// JWT claim values for the synthetic user.
	jwtProvider string
	jwtEmail    string
	jwtName     string
	// Client-callback allowlist. Default includes http://localhost:4200/callback.
	clientCallbackAllow []string
	// Custom audit writer. If nil, a fresh memorySystemAuditWriter is used.
	auditWriter SystemAuditWriter
	// Cookie options. Default Enabled=true.
	cookieOpts CookieOptions
	// registryOnlyProviders are wired into a DefaultProviderRegistry but
	// deliberately omitted from the static config.OAuth.Providers map. This
	// reproduces the production divergence where a provider (e.g. the
	// runtime-registered "tmi" dev provider) is resolvable via the registry
	// but not the YAML snapshot. When non-empty, the harness sets a registry.
	registryOnlyProviders map[string]OAuthProviderConfig
}

// withProvider sets the providers map to contain only this provider entry.
// Subsequent withProvider calls add additional entries via withAlsoProvider.
func withProvider(id string, cfg OAuthProviderConfig) stepUpHarnessOpt {
	return func(c *stepUpHarnessConfig) {
		c.providers = map[string]OAuthProviderConfig{id: cfg}
	}
}

// withAlsoProvider adds a provider entry without clearing the existing map.
// Use after a withProvider call to register additional providers.
func withAlsoProvider(id string, cfg OAuthProviderConfig) stepUpHarnessOpt {
	return func(c *stepUpHarnessConfig) {
		if c.providers == nil {
			c.providers = map[string]OAuthProviderConfig{}
		}
		c.providers[id] = cfg
	}
}

func withJWTSubject(sub string) stepUpHarnessOpt {
	return func(c *stepUpHarnessConfig) { c.jwtSubject = sub }
}

func withJWTIdentity(provider, email, name string) stepUpHarnessOpt {
	return func(c *stepUpHarnessConfig) {
		c.jwtProvider = provider
		c.jwtEmail = email
		c.jwtName = name
	}
}

func withAuditWriter(w SystemAuditWriter) stepUpHarnessOpt {
	return func(c *stepUpHarnessConfig) { c.auditWriter = w }
}

// withRegistryOnlyProvider wires a provider into the DefaultProviderRegistry
// while leaving it out of the static config.OAuth.Providers map. This models
// the runtime-registered "tmi" dev provider, which getProviderWithContext
// resolves from the registry but providerConfig (pre-fix) could not find in
// the YAML snapshot — previously a 500 on /oauth2/step_up.
func withRegistryOnlyProvider(id string, cfg OAuthProviderConfig) stepUpHarnessOpt {
	return func(c *stepUpHarnessConfig) {
		if c.registryOnlyProviders == nil {
			c.registryOnlyProviders = map[string]OAuthProviderConfig{}
		}
		c.registryOnlyProviders[id] = cfg
	}
}

func strongProviderConfig() OAuthProviderConfig {
	return OAuthProviderConfig{
		ID:               "google",
		Name:             "Google",
		Enabled:          true,
		Issuer:           "https://accounts.google.com",
		JWKSURL:          "https://www.googleapis.com/oauth2/v3/certs",
		ClientID:         "test-cid",
		ClientSecret:     "test-sec",
		AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:         "https://oauth2.googleapis.com/token",
		Scopes:           []string{"openid", "email"},
	}
}

func weakProviderConfig() OAuthProviderConfig {
	return OAuthProviderConfig{
		ID:               "github",
		Name:             "GitHub",
		Enabled:          true,
		ClientID:         "gh-cid",
		ClientSecret:     "gh-sec",
		AuthorizationURL: "https://github.com/login/oauth/authorize",
		TokenURL:         "https://github.com/login/oauth/access_token",
		Scopes:           []string{"read:user"},
	}
}

// newStepUpTestHarness builds a fully-wired Handlers with miniredis + an in-memory audit writer.
// Defaults: provider "google" (strong), one user "alice@example.com" with InternalUUID,
// client_callback allowlist allows http://localhost:4200/callback.
func newStepUpTestHarness(t *testing.T, opts ...stepUpHarnessOpt) *stepUpTestHarness {
	t.Helper()

	cfg := &stepUpHarnessConfig{
		providers:           map[string]OAuthProviderConfig{"google": strongProviderConfig()},
		jwtProvider:         "google",
		jwtEmail:            "alice@example.com",
		jwtName:             "Alice",
		clientCallbackAllow: []string{"http://localhost:4200/callback"},
		cookieOpts:          CookieOptions{Enabled: true, ExpiresIn: 3600, RefreshTTL: 86400},
	}
	for _, o := range opts {
		o(cfg)
	}

	mr, err := miniredis.Run()
	require.NoError(t, err)
	parts := strings.SplitN(mr.Addr(), ":", 2)

	dbManager := db.NewManager()
	require.NoError(t, dbManager.InitRedis(db.RedisConfig{Host: parts[0], Port: parts[1]}))

	keyManager, err := NewJWTKeyManager(JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-step-up-secret",
	})
	require.NoError(t, err)

	// Seed a single user under the configured provider/email. If the test
	// supplies a custom JWT subject (e.g., "sa:..." for CC tests), seed BOTH
	// the JWT subject as ProviderUserID AND keep the canonical user UUID so
	// tests can resolve via GetByProviderID.
	userUUID := uuid.New().String()
	jwtProviderUID := "uid-" + strings.ReplaceAll(cfg.jwtEmail, "@", "-")
	if cfg.jwtSubject != "" {
		jwtProviderUID = cfg.jwtSubject
	}
	u := repository.User{
		InternalUUID:   userUUID,
		Provider:       cfg.jwtProvider,
		ProviderUserID: jwtProviderUID,
		Email:          cfg.jwtEmail,
		Name:           cfg.jwtName,
		EmailVerified:  true,
		CreatedAt:      time.Now(),
		ModifiedAt:     time.Now(),
	}
	userRepo := &stepUpStubUserRepo{
		byProviderID: map[string]*repository.User{cfg.jwtProvider + "|" + jwtProviderUID: &u},
		byID:         map[string]*repository.User{userUUID: &u},
	}

	authCfg := Config{
		JWT: JWTConfig{
			SigningMethod:     "HS256",
			Secret:            "test-step-up-secret",
			ExpirationSeconds: 3600,
		},
		OAuth: OAuthConfig{
			CallbackURL:             "http://localhost:8080/oauth2/callback",
			ClientCallbackAllowList: cfg.clientCallbackAllow,
			Providers:               cfg.providers,
		},
	}

	svc := &Service{
		dbManager:  dbManager,
		keyManager: keyManager,
		config:     authCfg,
		userRepo:   userRepo,
		stateStore: NewInMemoryStateStore(),
	}

	var auditWriter SystemAuditWriter = &memorySystemAuditWriter{}
	if cfg.auditWriter != nil {
		auditWriter = cfg.auditWriter
	}
	auditor := NewStepUpAuditor(auditWriter)

	h := &Handlers{
		service:       svc,
		config:        authCfg,
		stepUpAuditor: auditor,
		cookieOpts:    cfg.cookieOpts,
	}

	// Wire a registry when the test asked for registry-only providers. The
	// registry's config source includes BOTH the static providers and the
	// registry-only ones, mirroring production where getProviderWithContext
	// resolves via the registry. The static config.OAuth.Providers map keeps
	// only cfg.providers, so a registry-only provider is absent from it.
	if len(cfg.registryOnlyProviders) > 0 {
		registryProviders := make(map[string]OAuthProviderConfig, len(cfg.providers)+len(cfg.registryOnlyProviders))
		for k, v := range cfg.providers {
			registryProviders[k] = v
		}
		for k, v := range cfg.registryOnlyProviders {
			registryProviders[k] = v
		}
		registry := NewDefaultProviderRegistry(registryProviders, nil, &mockSettingsReader{})
		h.SetProviderRegistry(registry)
		svc.SetProviderRegistry(registry)
	}

	// Mint the test JWT. If a custom subject was requested, build the JWT
	// directly via the key manager so the Subject claim is whatever the test
	// asked for (cannot use GenerateTokensWithUserInfo, which derives Subject
	// from user.ProviderUserID).
	var testJWT string
	if cfg.jwtSubject != "" {
		issuer := svc.deriveIssuer()
		now := time.Now()
		claims := &Claims{
			Email:            cfg.jwtEmail,
			EmailVerified:    true,
			Name:             cfg.jwtName,
			IdentityProvider: cfg.jwtProvider,
			AuthTime:         jwt.NewNumericDate(now),
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    issuer,
				Subject:   cfg.jwtSubject,
				Audience:  jwt.ClaimStrings{issuer},
				ExpiresAt: jwt.NewNumericDate(now.Add(3600 * time.Second)),
				IssuedAt:  jwt.NewNumericDate(now),
				NotBefore: jwt.NewNumericDate(now),
				ID:        uuid.New().String(),
			},
		}
		tokenStr, err := keyManager.CreateToken(claims)
		require.NoError(t, err)
		testJWT = tokenStr
	} else {
		userObj := convertRepoUserToServiceUser(&u)
		tokens, err := svc.GenerateTokensWithUserInfo(context.Background(), userObj, nil)
		require.NoError(t, err)
		testJWT = tokens.AccessToken
	}

	// Keep a typed reference to the in-memory writer if applicable.
	mw, _ := auditWriter.(*memorySystemAuditWriter)
	return &stepUpTestHarness{
		handlers:     h,
		testJWT:      testJWT,
		auditW:       mw,
		mr:           mr,
		originalUser: u,
		cleanup: func() {
			_ = dbManager.Close()
			mr.Close()
		},
	}
}

func TestStepUp_StrongProvider_Returns302WithPromptLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk&code_challenge_method=S256",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)

	h.handlers.StepUp(c)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d body=%s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "prompt=login") {
		t.Errorf("Location missing prompt=login: %s", loc)
	}
	if !strings.Contains(loc, "max_age=0") {
		t.Errorf("Location missing max_age=0: %s", loc)
	}
	if !strings.Contains(loc, "accounts.google.com") {
		t.Errorf("Location should be absolute upstream URL: %s", loc)
	}
}

// TestStepUp_RegistryOnlyProvider_NoFalse500 is the regression test for the
// /oauth2/step_up 500 found by CATS fuzzing. The handler resolved the provider
// twice: getProviderWithContext via the DB-backed registry (which knew the
// runtime-registered provider) and providerConfig via the static YAML snapshot
// (which did not). The mismatch caused providerConfig to fail and the handler
// to return 500 server_error — even for a valid HappyPath request. After the
// fix, providerConfig resolves from the same registry, so a registry-only
// strong provider follows the normal strong path (302 redirect upstream) and
// never returns 500.
func TestStepUp_RegistryOnlyProvider_NoFalse500(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// "tmi" models the dev provider registered at runtime via the registry but
	// absent from config.OAuth.Providers. Strong so the happy path redirects.
	tmiProvider := strongProviderConfig()
	tmiProvider.ID = "tmi"
	tmiProvider.Name = "TMI"

	h := newStepUpTestHarness(t,
		// Static config has NO providers; the only provider lives in the registry.
		withProvider("placeholder-unused", OAuthProviderConfig{ID: "placeholder-unused"}),
		withRegistryOnlyProvider("tmi", tmiProvider),
		withJWTIdentity("tmi", "alice@tmi.local", "Alice (TMI User)"),
	)
	defer h.cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk&code_challenge_method=S256",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)

	h.handlers.StepUp(c)

	// The core assertion: the registry/config divergence must NOT surface as 500.
	require.NotEqual(t, http.StatusInternalServerError, w.Code,
		"registry-only provider must not yield a 500; body=%s", w.Body.String())
	// A strong registry-only provider should follow the normal strong path.
	require.Equal(t, http.StatusFound, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Header().Get("Location"), "prompt=login")
}

func TestStepUp_MissingJWT_Returns401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		nil)
	// NO Authorization header, no cookie.
	h.handlers.StepUp(c)

	require.Equal(t, http.StatusUnauthorized, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "invalid_token")
}

func TestStepUp_CCGrant_Returns400UnsupportedGrantType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h := newStepUpTestHarness(t,
		withAuditWriter(auditW),
		withJWTSubject("sa:cc-grant-123:alice@example.com"),
	)
	defer h.cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)
	h.handlers.StepUp(c)

	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "unsupported_grant_type")
	require.Len(t, auditW.entries, 1)
	require.Equal(t, "auth.step_up_rejected", auditW.entries[0].FieldPath)
}

func TestStepUp_InvalidClientCallback_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Fevil.example%2F&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)
	h.handlers.StepUp(c)

	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "invalid_request")
}

func TestStepUp_MissingPKCE_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)
	h.handlers.StepUp(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestStepUp_WeakProvider_ShortCircuits200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h := newStepUpTestHarness(t,
		withProvider("github", weakProviderConfig()),
		withJWTIdentity("github", "bob@example.com", "Bob"),
		withAuditWriter(auditW),
	)
	defer h.cleanup()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/step_up?client_callback=http%3A%2F%2Flocalhost%3A4200%2Fcallback&code_challenge=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
		nil)
	c.Request.Header.Set("Authorization", "Bearer "+h.testJWT)
	h.handlers.StepUp(c)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "step_up_weak_complete")

	// New HttpOnly cookies must be set in response.
	cookies := w.Result().Cookies()
	var sawAccess, sawRefresh bool
	for _, ck := range cookies {
		if ck.Name == AccessTokenCookieName {
			sawAccess = true
			require.True(t, ck.HttpOnly, "access cookie missing HttpOnly")
		}
		if ck.Name == RefreshTokenCookieName {
			sawRefresh = true
			require.True(t, ck.HttpOnly, "refresh cookie missing HttpOnly")
		}
	}
	require.True(t, sawAccess, "expected new access cookie")
	require.True(t, sawRefresh, "expected new refresh cookie")

	// Audit row should record strength=weak.
	require.Len(t, auditW.entries, 1)
	require.Equal(t, "auth.step_up_complete", auditW.entries[0].FieldPath)
	require.Contains(t, *auditW.entries[0].NewValueRedacted, `"strength":"weak"`)
}

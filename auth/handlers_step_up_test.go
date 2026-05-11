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

// newStepUpTestHarness builds a fully-wired Handlers with miniredis + an in-memory audit writer.
// Defaults: provider "google" (strong), one user "alice@example.com" with InternalUUID,
// client_callback allowlist allows http://localhost:4200/callback.
func newStepUpTestHarness(t *testing.T) *stepUpTestHarness {
	t.Helper()

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

	// Seed user.
	aliceUUID := uuid.New().String()
	alice := repository.User{
		InternalUUID:   aliceUUID,
		Provider:       "google",
		ProviderUserID: "uid-alice",
		Email:          "alice@example.com",
		Name:           "Alice",
		EmailVerified:  true,
		CreatedAt:      time.Now(),
		ModifiedAt:     time.Now(),
	}
	userRepo := &stepUpStubUserRepo{
		byProviderID: map[string]*repository.User{"google|uid-alice": &alice},
		byID:         map[string]*repository.User{aliceUUID: &alice},
	}

	googleCfg := OAuthProviderConfig{
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

	cfg := Config{
		JWT: JWTConfig{
			SigningMethod:     "HS256",
			Secret:            "test-step-up-secret",
			ExpirationSeconds: 3600,
		},
		OAuth: OAuthConfig{
			CallbackURL:             "http://localhost:8080/oauth2/callback",
			ClientCallbackAllowList: []string{"http://localhost:4200/callback"},
			Providers:               map[string]OAuthProviderConfig{"google": googleCfg},
		},
	}

	svc := &Service{
		dbManager:  dbManager,
		keyManager: keyManager,
		config:     cfg,
		userRepo:   userRepo,
		stateStore: NewInMemoryStateStore(),
	}

	auditW := &memorySystemAuditWriter{}
	auditor := NewStepUpAuditor(auditW)

	h := &Handlers{
		service:       svc,
		config:        cfg,
		stepUpAuditor: auditor,
	}

	// Mint a JWT for alice (provider-scoped, fresh auth_time).
	userObj := convertRepoUserToServiceUser(&alice)
	tokens, err := svc.GenerateTokensWithUserInfo(context.Background(), userObj, nil)
	require.NoError(t, err)

	return &stepUpTestHarness{
		handlers:     h,
		testJWT:      tokens.AccessToken,
		auditW:       auditW,
		mr:           mr,
		originalUser: alice,
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

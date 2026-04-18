package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contentTypePlainText is a test-only constant to avoid goconst warnings.
const contentTypePlainText = "text/plain"

// =============================================================================
// Stub provider for DelegatedSource tests
// =============================================================================

type refreshStubProvider struct {
	id           string
	refreshResp  *ContentOAuthTokenResponse
	refreshErr   error
	refreshCalls atomic.Int32
}

func (p *refreshStubProvider) ID() string                             { return p.id }
func (p *refreshStubProvider) AuthorizationURL(_, _, _ string) string { return "" }
func (p *refreshStubProvider) ExchangeCode(_ context.Context, _, _, _ string) (*ContentOAuthTokenResponse, error) {
	return nil, errors.New("not implemented")
}
func (p *refreshStubProvider) Refresh(_ context.Context, _ string) (*ContentOAuthTokenResponse, error) {
	p.refreshCalls.Add(1)
	return p.refreshResp, p.refreshErr
}
func (p *refreshStubProvider) Revoke(_ context.Context, _ string) error { return nil }
func (p *refreshStubProvider) RequiredScopes() []string                 { return nil }
func (p *refreshStubProvider) FetchAccountInfo(_ context.Context, _ string) (string, string, error) {
	return "", "", nil
}

// =============================================================================
// In-memory mock repo for DelegatedSource tests (serialized RefreshWithLock)
// =============================================================================

// delegatedTestRepo is a simple in-memory content-token repository whose
// RefreshWithLock uses a mutex to serialize concurrent calls (mimicking the
// SELECT … FOR UPDATE behaviour of the GORM implementation).
type delegatedTestRepo struct {
	mu     sync.Mutex
	tokens map[string]*ContentToken // keyed by ID
}

func newDelegatedTestRepo() *delegatedTestRepo {
	return &delegatedTestRepo{tokens: make(map[string]*ContentToken)}
}

func (r *delegatedTestRepo) store(t *ContentToken) {
	r.tokens[t.ID] = t
}

func (r *delegatedTestRepo) GetByUserAndProvider(_ context.Context, userID, providerID string) (*ContentToken, error) {
	for _, t := range r.tokens {
		if t.UserID == userID && t.ProviderID == providerID {
			cp := *t
			return &cp, nil
		}
	}
	return nil, ErrContentTokenNotFound
}

func (r *delegatedTestRepo) ListByUser(_ context.Context, _ string) ([]ContentToken, error) {
	return nil, nil
}

func (r *delegatedTestRepo) Upsert(_ context.Context, t *ContentToken) error {
	r.tokens[t.ID] = t
	return nil
}

func (r *delegatedTestRepo) UpdateStatus(_ context.Context, id, status, lastError string) error {
	t, ok := r.tokens[id]
	if !ok {
		return ErrContentTokenNotFound
	}
	t.Status = status
	t.LastError = lastError
	return nil
}

func (r *delegatedTestRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.tokens[id]; !ok {
		return ErrContentTokenNotFound
	}
	delete(r.tokens, id)
	return nil
}

func (r *delegatedTestRepo) DeleteByUserAndProvider(_ context.Context, userID, providerID string) (*ContentToken, error) {
	for id, t := range r.tokens {
		if t.UserID == userID && t.ProviderID == providerID {
			cp := *t
			delete(r.tokens, id)
			return &cp, nil
		}
	}
	return nil, ErrContentTokenNotFound
}

// RefreshWithLock uses a mutex to serialize concurrent calls. The fn callback
// receives a copy of the current token, and if it returns (next, nil) the
// stored token is replaced with next. If fn returns an error, nothing is
// persisted (mirrors the GORM transaction-rollback semantics).
func (r *delegatedTestRepo) RefreshWithLock(_ context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	t, ok := r.tokens[id]
	if !ok {
		return nil, ErrContentTokenNotFound
	}
	cp := *t
	next, err := fn(&cp)
	if err != nil {
		return nil, err
	}
	r.tokens[id] = next
	result := *next
	return &result, nil
}

// =============================================================================
// Helper
// =============================================================================

func futureTime(d time.Duration) *time.Time {
	t := time.Now().Add(d)
	return &t
}

func pastTime(d time.Duration) *time.Time {
	t := time.Now().Add(-d)
	return &t
}

// =============================================================================
// Tests
// =============================================================================

// Test 1: No token stored → ErrAuthRequired
func TestDelegatedSource_FetchForUser_NoToken_ReturnsAuthRequired(t *testing.T) {
	repo := newDelegatedTestRepo()
	registry := NewContentOAuthProviderRegistry()

	ds := &DelegatedSource{
		ProviderID: "confluence",
		Tokens:     repo,
		Registry:   registry,
		DoFetch: func(_ context.Context, _, _ string) ([]byte, string, error) {
			return []byte("data"), contentTypePlainText, nil
		},
	}

	_, _, err := ds.FetchForUser(context.Background(), "user-1", "https://example.com/page")
	assert.ErrorIs(t, err, ErrAuthRequired)
}

// Test 2: Valid non-expired token → DoFetch receives plaintext access token
func TestDelegatedSource_FetchForUser_ValidToken_CallsDoFetch(t *testing.T) {
	repo := newDelegatedTestRepo()
	registry := NewContentOAuthProviderRegistry()

	repo.store(&ContentToken{
		ID:          "tok-1",
		UserID:      "user-1",
		ProviderID:  "confluence",
		AccessToken: "valid-access-token",
		ExpiresAt:   futureTime(1 * time.Hour),
		Status:      ContentTokenStatusActive,
	})

	var gotToken string
	ds := &DelegatedSource{
		ProviderID: "confluence",
		Tokens:     repo,
		Registry:   registry,
		DoFetch: func(_ context.Context, accessToken, _ string) ([]byte, string, error) {
			gotToken = accessToken
			return []byte("page content"), "text/html", nil
		},
	}

	data, ct, err := ds.FetchForUser(context.Background(), "user-1", "https://example.com/page")
	require.NoError(t, err)
	assert.Equal(t, "valid-access-token", gotToken)
	assert.Equal(t, []byte("page content"), data)
	assert.Equal(t, "text/html", ct)
}

// Test 3: Expired token → refresh then fetch with new access token
func TestDelegatedSource_FetchForUser_Expired_RefreshesThenFetches(t *testing.T) {
	repo := newDelegatedTestRepo()

	repo.store(&ContentToken{
		ID:           "tok-2",
		UserID:       "user-1",
		ProviderID:   "prov",
		AccessToken:  "old-access",
		RefreshToken: "my-refresh",
		ExpiresAt:    pastTime(10 * time.Minute),
		Status:       ContentTokenStatusActive,
	})

	stub := &refreshStubProvider{
		id: "prov",
		refreshResp: &ContentOAuthTokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(stub)

	var gotToken string
	ds := &DelegatedSource{
		ProviderID: "prov",
		Tokens:     repo,
		Registry:   registry,
		DoFetch: func(_ context.Context, accessToken, _ string) ([]byte, string, error) {
			gotToken = accessToken
			return []byte("refreshed"), contentTypePlainText, nil
		},
		Skew: 30 * time.Second,
	}

	data, _, err := ds.FetchForUser(context.Background(), "user-1", "https://example.com/doc")
	require.NoError(t, err)
	assert.Equal(t, "new-access", gotToken, "DoFetch should see the new access token")
	assert.Equal(t, []byte("refreshed"), data)
	assert.Equal(t, int32(1), stub.refreshCalls.Load())

	// Persisted token should have been updated
	stored, err := repo.GetByUserAndProvider(context.Background(), "user-1", "prov")
	require.NoError(t, err)
	assert.Equal(t, "new-access", stored.AccessToken)
	assert.Equal(t, "new-refresh", stored.RefreshToken)
	assert.Equal(t, ContentTokenStatusActive, stored.Status)
}

// Test 4: Permanent refresh failure → token marked failed_refresh; subsequent
// call also returns ErrAuthRequired without hitting the provider again.
func TestDelegatedSource_FetchForUser_Expired_PermanentRefreshFailure_ReturnsAuthRequired(t *testing.T) {
	repo := newDelegatedTestRepo()

	repo.store(&ContentToken{
		ID:           "tok-3",
		UserID:       "user-1",
		ProviderID:   "prov",
		AccessToken:  "old-access",
		RefreshToken: "bad-refresh",
		ExpiresAt:    pastTime(10 * time.Minute),
		Status:       ContentTokenStatusActive,
	})

	stub := &refreshStubProvider{
		id: "prov",
		// permanent error (e.g. invalid_grant)
		refreshErr: &errContentOAuthPermanent{msg: "invalid_grant"},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(stub)

	ds := &DelegatedSource{
		ProviderID: "prov",
		Tokens:     repo,
		Registry:   registry,
		DoFetch: func(_ context.Context, _, _ string) ([]byte, string, error) {
			t.Fatal("DoFetch should not be called on refresh failure")
			return nil, "", nil
		},
		Skew: 30 * time.Second,
	}

	// First call: triggers refresh, gets permanent failure
	_, _, err := ds.FetchForUser(context.Background(), "user-1", "https://example.com/doc")
	assert.ErrorIs(t, err, ErrAuthRequired)
	assert.Equal(t, int32(1), stub.refreshCalls.Load())

	// Token should be flipped to failed_refresh
	stored, lookupErr := repo.GetByUserAndProvider(context.Background(), "user-1", "prov")
	require.NoError(t, lookupErr)
	assert.Equal(t, ContentTokenStatusFailedRefresh, stored.Status)

	// Second call: should short-circuit on status=failed_refresh, no provider call
	_, _, err2 := ds.FetchForUser(context.Background(), "user-1", "https://example.com/doc")
	assert.ErrorIs(t, err2, ErrAuthRequired)
	assert.Equal(t, int32(1), stub.refreshCalls.Load(), "provider should NOT be called again")
}

// Test 5: Transient refresh failure → token NOT flipped to failed_refresh
func TestDelegatedSource_FetchForUser_Expired_TransientRefreshFailure_ReturnsErrTransient(t *testing.T) {
	repo := newDelegatedTestRepo()

	repo.store(&ContentToken{
		ID:           "tok-4",
		UserID:       "user-1",
		ProviderID:   "prov",
		AccessToken:  "old-access",
		RefreshToken: "my-refresh",
		ExpiresAt:    pastTime(10 * time.Minute),
		Status:       ContentTokenStatusActive,
	})

	stub := &refreshStubProvider{
		id:         "prov",
		refreshErr: errors.New("upstream 503"),
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(stub)

	ds := &DelegatedSource{
		ProviderID: "prov",
		Tokens:     repo,
		Registry:   registry,
		DoFetch: func(_ context.Context, _, _ string) ([]byte, string, error) {
			t.Fatal("DoFetch should not be called on refresh failure")
			return nil, "", nil
		},
		Skew: 30 * time.Second,
	}

	_, _, err := ds.FetchForUser(context.Background(), "user-1", "https://example.com/doc")
	assert.ErrorIs(t, err, ErrTransient)

	// Token status should remain active (not failed_refresh)
	stored, lookupErr := repo.GetByUserAndProvider(context.Background(), "user-1", "prov")
	require.NoError(t, lookupErr)
	assert.Equal(t, ContentTokenStatusActive, stored.Status)
}

// Test 6: Token already in failed_refresh status → ErrAuthRequired without any provider call
func TestDelegatedSource_FetchForUser_TokenStatusFailedRefresh_ReturnsAuthRequired_WithoutProviderCall(t *testing.T) {
	repo := newDelegatedTestRepo()

	// Token is expired AND in failed_refresh state
	repo.store(&ContentToken{
		ID:          "tok-5",
		UserID:      "user-1",
		ProviderID:  "prov",
		AccessToken: "stale",
		ExpiresAt:   pastTime(10 * time.Minute),
		Status:      ContentTokenStatusFailedRefresh,
	})

	stub := &refreshStubProvider{id: "prov"}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(stub)

	ds := &DelegatedSource{
		ProviderID: "prov",
		Tokens:     repo,
		Registry:   registry,
		DoFetch: func(_ context.Context, _, _ string) ([]byte, string, error) {
			t.Fatal("DoFetch should not be called when status=failed_refresh")
			return nil, "", nil
		},
	}

	_, _, err := ds.FetchForUser(context.Background(), "user-1", "https://example.com/doc")
	assert.ErrorIs(t, err, ErrAuthRequired)
	assert.Equal(t, int32(0), stub.refreshCalls.Load(), "provider.Refresh must not be called")
}

// Test 7: Concurrent goroutines all fetch with expired token → exactly one provider call
func TestDelegatedSource_ConcurrentExpired_OnlyOneProviderCall(t *testing.T) {
	repo := newDelegatedTestRepo()

	repo.store(&ContentToken{
		ID:           "tok-6",
		UserID:       "user-1",
		ProviderID:   "prov",
		AccessToken:  "old-access",
		RefreshToken: "my-refresh",
		ExpiresAt:    pastTime(10 * time.Minute),
		Status:       ContentTokenStatusActive,
	})

	stub := &refreshStubProvider{
		id: "prov",
		refreshResp: &ContentOAuthTokenResponse{
			AccessToken:  "fresh-access",
			RefreshToken: "fresh-refresh",
			ExpiresIn:    3600,
		},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(stub)

	var fetchCount atomic.Int32
	ds := &DelegatedSource{
		ProviderID: "prov",
		Tokens:     repo,
		Registry:   registry,
		DoFetch: func(_ context.Context, _, _ string) ([]byte, string, error) {
			fetchCount.Add(1)
			return []byte("ok"), contentTypePlainText, nil
		},
		Skew: 30 * time.Second,
	}

	const goroutines = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, _, errs[i] = ds.FetchForUser(context.Background(), "user-1", "https://example.com/doc")
		}()
	}
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d should succeed", i)
	}
	assert.Equal(t, int32(1), stub.refreshCalls.Load(), "provider.Refresh should be called exactly once")
	assert.Equal(t, int32(goroutines), fetchCount.Load(), "DoFetch should be called by all goroutines")
}

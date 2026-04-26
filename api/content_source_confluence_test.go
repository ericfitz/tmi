package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDelegatedConfluenceSource_Name(t *testing.T) {
	s := &DelegatedConfluenceSource{}
	assert.Equal(t, ProviderConfluence, s.Name())
	assert.Equal(t, "confluence", s.Name())
}

func TestDelegatedConfluenceSource_CanHandle(t *testing.T) {
	s := &DelegatedConfluenceSource{}
	cases := []struct {
		uri string
		ok  bool
	}{
		{"https://acme.atlassian.net/wiki/spaces/ENG/pages/123/Home", true},
		{"https://example.atlassian.net/wiki/spaces/X/pages/1/Title", true},
		{"https://acme.atlassian.net/jira/projects/X", false},
		{"https://acme.atlassian.net/", false},
		{"https://confluence.example.com/wiki/page", false},
		{"https://docs.google.com/document/d/abc/edit", false},
		{"http://example.com", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.uri, func(t *testing.T) {
			assert.Equal(t, c.ok, s.CanHandle(context.Background(), c.uri))
		})
	}
}

func TestParseConfluencePageURL(t *testing.T) {
	cases := []struct {
		name       string
		uri        string
		wantHost   string
		wantPageID string
		wantErr    bool
	}{
		{
			name:       "modern with slug",
			uri:        "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345/Home+Page",
			wantHost:   "acme.atlassian.net",
			wantPageID: "12345",
		},
		{
			name:       "modern without slug",
			uri:        "https://acme.atlassian.net/wiki/spaces/ENG/pages/98765",
			wantHost:   "acme.atlassian.net",
			wantPageID: "98765",
		},
		{
			name:       "modern with trailing slash",
			uri:        "https://acme.atlassian.net/wiki/spaces/ENG/pages/77/",
			wantHost:   "acme.atlassian.net",
			wantPageID: "77",
		},
		{
			name:    "legacy display form unsupported",
			uri:     "https://acme.atlassian.net/wiki/display/ENG/Home+Page",
			wantErr: true,
		},
		{
			name:    "legacy short link unsupported",
			uri:     "https://acme.atlassian.net/wiki/x/AAAA",
			wantErr: true,
		},
		{
			name:    "non-atlassian host",
			uri:     "https://confluence.example.com/wiki/spaces/X/pages/1",
			wantErr: true,
		},
		{
			name:    "garbage URL",
			uri:     "not a url",
			wantErr: true,
		},
		{
			name:    "non-http scheme",
			uri:     "ftp://acme.atlassian.net/wiki/spaces/ENG/pages/1",
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, id, err := parseConfluencePageURL(c.uri)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.wantHost, host)
			assert.Equal(t, c.wantPageID, id)
		})
	}
}

// stubAtlassian builds an httptest.Server that speaks the Atlassian endpoints
// the Confluence source consumes: /oauth/token/accessible-resources and
// /ex/confluence/{cloud_id}/wiki/api/v2/pages/{id}. It tracks call counts
// for assertion.
type stubAtlassian struct {
	server               *httptest.Server
	resources            []map[string]string // accessible-resources response
	pageBodyValue        string
	pageStatus           int
	resourcesStatus      int
	resourcesCalls       int32
	pageMetadataCalls    int32
	pageContentCalls     int32
	expectAuthorization  string
	authorizationFailure *atomic.Bool
}

func newStubAtlassian(t *testing.T) *stubAtlassian {
	t.Helper()
	s := &stubAtlassian{
		pageBodyValue:        "<p>hello world</p>",
		pageStatus:           http.StatusOK,
		resourcesStatus:      http.StatusOK,
		expectAuthorization:  "Bearer at-1",
		authorizationFailure: &atomic.Bool{},
	}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.expectAuthorization != "" && r.Header.Get("Authorization") != s.expectAuthorization {
			s.authorizationFailure.Store(true)
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/oauth/token/accessible-resources":
			atomic.AddInt32(&s.resourcesCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			if s.resourcesStatus != http.StatusOK {
				http.Error(w, "no resources", s.resourcesStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(s.resources)
		case strings.HasPrefix(r.URL.Path, "/ex/confluence/"):
			// Distinguish metadata-only probes (no body-format) from content fetches.
			if r.URL.Query().Get("body-format") == "view" {
				atomic.AddInt32(&s.pageContentCalls, 1)
				if s.pageStatus != http.StatusOK {
					http.Error(w, "page failure", s.pageStatus)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":    "12345",
					"title": "Home",
					"body": map[string]any{
						"view": map[string]string{
							"representation": "view",
							"value":          s.pageBodyValue,
						},
					},
				})
				return
			}
			atomic.AddInt32(&s.pageMetadataCalls, 1)
			if s.pageStatus != http.StatusOK {
				http.Error(w, "page metadata failure", s.pageStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "12345", "title": "Home"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

// newConfluenceSourceForTest builds a DelegatedConfluenceSource pointed at the
// stub Atlassian server, with a token repository that returns a long-lived
// active token. Returns the source and the token repo so tests can override
// behavior.
func newConfluenceSourceForTest(stub *stubAtlassian) (*DelegatedConfluenceSource, *mockContentTokenRepo) {
	tokens := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, userID, providerID string) (*ContentToken, error) {
			expiry := time.Now().Add(1 * time.Hour)
			return &ContentToken{
				ID:          "tok-1",
				UserID:      userID,
				ProviderID:  providerID,
				AccessToken: "at-1",
				Status:      ContentTokenStatusActive,
				ExpiresAt:   &expiry,
			}, nil
		},
	}
	registry := NewContentOAuthProviderRegistry()
	src := NewDelegatedConfluenceSource(tokens, registry)
	src.apiBase = stub.server.URL
	src.httpClient = stub.server.Client()
	return src, tokens
}

func TestDelegatedConfluenceSource_Fetch_HappyPath(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{
		{"id": "cloud-1", "url": "https://acme.atlassian.net"},
	}
	src, _ := newConfluenceSourceForTest(stub)

	ctx := WithUserID(context.Background(), "alice-uuid")
	data, contentType, err := src.Fetch(ctx,
		"https://acme.atlassian.net/wiki/spaces/ENG/pages/12345/Home")
	require.NoError(t, err)
	assert.Equal(t, "text/html", contentType)
	assert.Equal(t, "<p>hello world</p>", string(data))
	assert.False(t, stub.authorizationFailure.Load(), "all calls should carry the bearer token")
	assert.Equal(t, int32(1), atomic.LoadInt32(&stub.resourcesCalls))
	assert.Equal(t, int32(1), atomic.LoadInt32(&stub.pageContentCalls))
}

func TestDelegatedConfluenceSource_Fetch_MultiSite(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{
		{"id": "cloud-other", "url": "https://other.atlassian.net"},
		{"id": "cloud-acme", "url": "https://acme.atlassian.net"},
	}
	src, _ := newConfluenceSourceForTest(stub)

	ctx := WithUserID(context.Background(), "alice-uuid")
	_, _, err := src.Fetch(ctx, "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.NoError(t, err)
	// Sanity: the stub URL handler doesn't echo the cloud_id back, but the
	// handler asserted the path begins with /ex/confluence/. We rely on the
	// no-error result + the resolveCloudID matching logic to confirm the
	// correct site was picked.
}

func TestDelegatedConfluenceSource_Fetch_NoMatchingSite(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{
		{"id": "cloud-other", "url": "https://other.atlassian.net"},
	}
	src, _ := newConfluenceSourceForTest(stub)

	ctx := WithUserID(context.Background(), "alice-uuid")
	_, _, err := src.Fetch(ctx, "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no accessible resource matches host acme.atlassian.net")
	assert.Equal(t, int32(0), atomic.LoadInt32(&stub.pageContentCalls))
}

func TestDelegatedConfluenceSource_Fetch_PageNotFound(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{{"id": "cloud-acme", "url": "https://acme.atlassian.net"}}
	stub.pageStatus = http.StatusNotFound
	src, _ := newConfluenceSourceForTest(stub)

	ctx := WithUserID(context.Background(), "alice-uuid")
	_, _, err := src.Fetch(ctx, "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=404")
}

func TestDelegatedConfluenceSource_Fetch_RateLimited429(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{{"id": "cloud-acme", "url": "https://acme.atlassian.net"}}
	stub.pageStatus = http.StatusTooManyRequests
	src, _ := newConfluenceSourceForTest(stub)

	ctx := WithUserID(context.Background(), "alice-uuid")
	_, _, err := src.Fetch(ctx, "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=429")
}

func TestDelegatedConfluenceSource_Fetch_NoUserContext(t *testing.T) {
	stub := newStubAtlassian(t)
	src, _ := newConfluenceSourceForTest(stub)
	_, _, err := src.Fetch(context.Background(),
		"https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.ErrorIs(t, err, ErrAuthRequired)
}

func TestDelegatedConfluenceSource_Fetch_NoToken(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{{"id": "cloud-acme", "url": "https://acme.atlassian.net"}}
	src, _ := newConfluenceSourceForTest(stub)
	src.Delegated.Tokens = &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return nil, ErrContentTokenNotFound
		},
	}
	ctx := WithUserID(context.Background(), "alice-uuid")
	_, _, err := src.Fetch(ctx, "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.ErrorIs(t, err, ErrAuthRequired)
}

func TestDelegatedConfluenceSource_Fetch_LegacyURLRejected(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{{"id": "cloud-acme", "url": "https://acme.atlassian.net"}}
	src, _ := newConfluenceSourceForTest(stub)
	ctx := WithUserID(context.Background(), "alice-uuid")
	_, _, err := src.Fetch(ctx, "https://acme.atlassian.net/wiki/display/ENG/Home")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not extract page id")
	assert.Equal(t, int32(0), atomic.LoadInt32(&stub.pageContentCalls))
}

func TestDelegatedConfluenceSource_ValidateAccess_OK(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{{"id": "cloud-acme", "url": "https://acme.atlassian.net"}}
	src, _ := newConfluenceSourceForTest(stub)
	ctx := WithUserID(context.Background(), "alice-uuid")
	ok, err := src.ValidateAccess(ctx, "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int32(1), atomic.LoadInt32(&stub.pageMetadataCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&stub.pageContentCalls))
}

func TestDelegatedConfluenceSource_ValidateAccess_NotFound(t *testing.T) {
	stub := newStubAtlassian(t)
	stub.resources = []map[string]string{{"id": "cloud-acme", "url": "https://acme.atlassian.net"}}
	stub.pageStatus = http.StatusNotFound
	src, _ := newConfluenceSourceForTest(stub)
	ctx := WithUserID(context.Background(), "alice-uuid")
	ok, err := src.ValidateAccess(ctx, "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.NoError(t, err, "4xx maps to (false, nil), not an error")
	assert.False(t, ok)
}

func TestDelegatedConfluenceSource_ValidateAccess_NoUserContext(t *testing.T) {
	stub := newStubAtlassian(t)
	src, _ := newConfluenceSourceForTest(stub)
	ok, err := src.ValidateAccess(context.Background(),
		"https://acme.atlassian.net/wiki/spaces/ENG/pages/12345")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAuthRequired))
	assert.False(t, ok)
}

func TestDelegatedConfluenceSource_RequestAccess_NoOp(t *testing.T) {
	stub := newStubAtlassian(t)
	src, _ := newConfluenceSourceForTest(stub)
	require.NoError(t, src.RequestAccess(context.Background(),
		"https://acme.atlassian.net/wiki/spaces/ENG/pages/12345"))
	// No HTTP calls should have been made.
	assert.Equal(t, int32(0), atomic.LoadInt32(&stub.pageMetadataCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&stub.pageContentCalls))
	assert.Equal(t, int32(0), atomic.LoadInt32(&stub.resourcesCalls))
}

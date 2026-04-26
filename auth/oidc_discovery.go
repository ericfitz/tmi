package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// OIDCDiscoveryDoc represents the subset of an OpenID Connect Discovery 1.0
// metadata document we need to classify an OAuth provider. Field names match
// the spec; only the fields we consume are declared.
type OIDCDiscoveryDoc struct {
	Issuer                 string   `json:"issuer"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint"`
	TokenEndpoint          string   `json:"token_endpoint"`
	UserinfoEndpoint       string   `json:"userinfo_endpoint"`
	JWKSURI                string   `json:"jwks_uri"`
	SubjectTypesSupported  []string `json:"subject_types_supported"`
	ResponseTypesSupported []string `json:"response_types_supported"`
}

// IsValid reports whether doc has the minimum fields required by the OIDC
// Discovery 1.0 spec. userinfo_endpoint is RECOMMENDED rather than REQUIRED;
// callers that need it should check separately.
func (d *OIDCDiscoveryDoc) IsValid() bool {
	return d.Issuer != "" &&
		d.AuthorizationEndpoint != "" &&
		d.TokenEndpoint != "" &&
		d.JWKSURI != "" &&
		len(d.SubjectTypesSupported) > 0 &&
		len(d.ResponseTypesSupported) > 0
}

const wellKnownPath = "/.well-known/openid-configuration"

type cachedEntry struct {
	doc       *OIDCDiscoveryDoc
	fetchedAt time.Time
}

type DiscoveryClient struct {
	httpClient *http.Client
	cacheTTL   time.Duration
	mu         sync.RWMutex
	cache      map[string]cachedEntry
	// sf collapses concurrent first-fetches for the same issuer into a single
	// upstream request. Without it, N goroutines that arrive before the cache
	// is populated would all miss and all fire HTTP. (Issue #292.)
	sf singleflight.Group
}

func NewDiscoveryClient(timeout, cacheTTL time.Duration) *DiscoveryClient {
	return &DiscoveryClient{
		httpClient: &http.Client{Timeout: timeout},
		cacheTTL:   cacheTTL,
		cache:      make(map[string]cachedEntry),
	}
}

// Discover fetches and validates the OIDC discovery doc for issuerURL.
// Returns a valid doc on success. Returns (nil, nil) when the issuer is not
// OIDC-compliant (404, network error, invalid JSON, or doc fails IsValid) —
// callers should treat nil-doc as "not OIDC" rather than as an error.
// Returns (nil, err) only for programmer errors (e.g. invalid issuerURL).
//
// Concurrent first-fetches for the same issuer collapse into a single upstream
// request via singleflight (#292); subsequent calls hit the cache.
func (c *DiscoveryClient) Discover(ctx context.Context, issuerURL string) (*OIDCDiscoveryDoc, error) {
	if issuerURL == "" {
		return nil, fmt.Errorf("issuerURL is empty")
	}

	c.mu.RLock()
	if entry, ok := c.cache[issuerURL]; ok && time.Since(entry.fetchedAt) < c.cacheTTL {
		c.mu.RUnlock()
		return entry.doc, nil
	}
	c.mu.RUnlock()

	// Cache miss — collapse concurrent fetches for the same issuer into a
	// single upstream request. The result is shared with all waiters; the
	// fetcher stores it in the cache before returning so later callers hit
	// the cache instead of taking the singleflight path again.
	res, err, _ := c.sf.Do(issuerURL, func() (any, error) {
		// Re-check the cache: another goroutine may have populated it while
		// we were waiting for the singleflight slot.
		c.mu.RLock()
		if entry, ok := c.cache[issuerURL]; ok && time.Since(entry.fetchedAt) < c.cacheTTL {
			c.mu.RUnlock()
			return entry.doc, nil
		}
		c.mu.RUnlock()
		return c.fetchAndCache(ctx, issuerURL)
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	doc, ok := res.(*OIDCDiscoveryDoc)
	if !ok {
		return nil, fmt.Errorf("singleflight returned unexpected type %T", res)
	}
	return doc, nil
}

// fetchAndCache performs the actual HTTP discovery request and stores the
// result (success or negative) in the cache. Returns (nil, nil) for any
// "not OIDC" condition; (nil, err) only for programmer errors.
func (c *DiscoveryClient) fetchAndCache(ctx context.Context, issuerURL string) (*OIDCDiscoveryDoc, error) {
	url := strings.TrimSuffix(issuerURL, "/") + wellKnownPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // G107: issuerURL is operator-configured by design
	if err != nil {
		c.storeCache(issuerURL, nil)
		return nil, nil // network error -> not OIDC
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		c.storeCache(issuerURL, nil)
		return nil, nil // non-200 -> not OIDC
	}

	var doc OIDCDiscoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		c.storeCache(issuerURL, nil)
		return nil, nil // invalid JSON -> not OIDC
	}

	if !doc.IsValid() {
		c.storeCache(issuerURL, nil)
		return nil, nil // missing required fields -> not OIDC
	}

	c.storeCache(issuerURL, &doc)
	return &doc, nil
}

func (c *DiscoveryClient) storeCache(issuerURL string, doc *OIDCDiscoveryDoc) {
	c.mu.Lock()
	c.cache[issuerURL] = cachedEntry{doc: doc, fetchedAt: time.Now()}
	c.mu.Unlock()
}

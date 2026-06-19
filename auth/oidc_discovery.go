package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// OIDCDiscoveryDoc represents the subset of an OpenID Connect Discovery 1.0
// metadata document we need to classify an OAuth provider. Field names match
// the spec; only the fields we consume are declared.
// SEM@5f9f526cf6b26f69441543993290f8ffaedac64a: subset of an OIDC Discovery 1.0 metadata document used to classify an OAuth provider (pure)
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
// SEM@5f9f526cf6b26f69441543993290f8ffaedac64a: validate that a discovery document has the minimum required OIDC fields (pure)
func (d *OIDCDiscoveryDoc) IsValid() bool {
	return d.Issuer != "" &&
		d.AuthorizationEndpoint != "" &&
		d.TokenEndpoint != "" &&
		d.JWKSURI != "" &&
		len(d.SubjectTypesSupported) > 0 &&
		len(d.ResponseTypesSupported) > 0
}

const wellKnownPath = "/.well-known/openid-configuration"

// SEM@2c76a87aa45ddc7300c5f084caf6a460714fab5d: cache record pairing an OIDC discovery document with its fetch timestamp (pure)
type cachedEntry struct {
	doc       *OIDCDiscoveryDoc
	fetchedAt time.Time
}

// SEM@5c1bcf212b418d73d444551267f29e1025089cb6: HTTP client with TTL cache and singleflight deduplication for OIDC discovery requests (mutates shared state)
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

// SEM@e55d63794c48585aafab36880122df63ab8ab1be: build an OIDC discovery client with redirect-refusing HTTP transport and a TTL cache (pure)
func NewDiscoveryClient(timeout, cacheTTL time.Duration) *DiscoveryClient {
	return &DiscoveryClient{
		// A redirecting issuer is classified as "not OIDC" (the fetch fails)
		// rather than followed to an arbitrary host (safehttp.RefuseRedirects),
		// and an internal/private issuer URL is blocked at dial time.
		httpClient: newProviderHTTPClient(timeout),
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
// SEM@5c1bcf212b418d73d444551267f29e1025089cb6: fetch and cache the OIDC discovery document for an issuer, collapsing concurrent requests (mutates shared state)
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
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: fetch the OIDC discovery document over HTTP and store the result in the cache (mutates shared state)
func (c *DiscoveryClient) fetchAndCache(ctx context.Context, issuerURL string) (*OIDCDiscoveryDoc, error) {
	url := strings.TrimSuffix(issuerURL, "/") + wellKnownPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOAuthResponseBytes)).Decode(&doc); err != nil {
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

// SEM@2c76a87aa45ddc7300c5f084caf6a460714fab5d: store an OIDC discovery document in the client cache under the issuer URL (mutates shared state)
func (c *DiscoveryClient) storeCache(issuerURL string, doc *OIDCDiscoveryDoc) {
	c.mu.Lock()
	c.cache[issuerURL] = cachedEntry{doc: doc, fetchedAt: time.Now()}
	c.mu.Unlock()
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// confluenceMaxBodySize caps response bodies (page content + accessible-resources)
// to bound memory exposure to a single fetch. The Confluence v2 API page-content
// payload for a typical page is well under this cap; the limit is defensive.
const confluenceMaxBodySize = 10 * 1024 * 1024 // 10 MiB

// confluenceAPIBase is the Atlassian REST API root used to address per-tenant
// Confluence resources via the cloud_id path prefix. Hard-coded — delegated
// sources do not proxy arbitrary user-supplied hosts.
const confluenceAPIBase = "https://api.atlassian.com"

// confluencePagePathRegex extracts the numeric page id from the modern
// Confluence Cloud URL form: /wiki/spaces/{SPACE}/pages/{id}[/{slug}].
var confluencePagePathRegex = regexp.MustCompile(`/wiki/spaces/[^/]+/pages/([0-9]+)(?:/|$)`)

// DelegatedConfluenceSource fetches Confluence Cloud page content under the
// authenticated user's identity, using a per-user OAuth token managed by the
// shared DelegatedSource helper.
//
// Construct via NewDelegatedConfluenceSource. A zero-value struct has no
// Delegated helper and will panic on Fetch.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: content source that fetches Confluence pages via delegated OAuth tokens
type DelegatedConfluenceSource struct {
	// Delegated is the shared DelegatedSource helper that handles token
	// lookup, lazy refresh, and status transitions.
	Delegated *DelegatedSource

	// client is the SafeHTTPClient used for Atlassian API calls. It enforces
	// the SSRF allowlist, scheme allowlist, DNS-pinning, and body cap for
	// every outbound call.
	client *SafeHTTPClient

	// apiBase is the Atlassian REST API root (overridable for tests).
	apiBase string
}

// NewDelegatedConfluenceSource constructs a Confluence delegated source wired
// to the given token repository and OAuth provider registry. validator MUST
// be non-nil; in production it is built from the operator's content-source
// allowlist (typically containing api.atlassian.com).
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: build a DelegatedConfluenceSource with a hardened HTTP client and token store (pure)
func NewDelegatedConfluenceSource(
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
	validator *URIValidator,
) *DelegatedConfluenceSource {
	s := &DelegatedConfluenceSource{
		client: NewSafeHTTPClient(
			validator,
			WithDefaultTimeouts(30*time.Second, 10*time.Second, confluenceMaxBodySize),
		),
		apiBase: confluenceAPIBase,
	}
	s.Delegated = &DelegatedSource{
		ProviderID: ProviderConfluence,
		Tokens:     tokens,
		Registry:   registry,
		DoFetch:    s.doFetchPageView,
	}
	return s
}

// Name returns the provider id "confluence".
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: return the canonical provider name for this source (pure)
func (s *DelegatedConfluenceSource) Name() string { return ProviderConfluence }

// CanHandle returns true for Confluence Cloud page URLs of the form
// https://*.atlassian.net/wiki/...
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: validate that a URI is a supported Atlassian Cloud Confluence page URL (pure)
func (s *DelegatedConfluenceSource) CanHandle(_ context.Context, uri string) bool {
	host, ok := parseConfluenceHost(uri)
	if !ok {
		return false
	}
	if !strings.HasSuffix(host, ".atlassian.net") {
		return false
	}
	lower := strings.ToLower(uri)
	return strings.Contains(lower, "/wiki/")
}

// Fetch returns the page's view-format HTML for the user in ctx. Requires
// UserIDFromContext to return a non-empty user id; delegated sources cannot
// run without user context.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: fetch Confluence page content for the authenticated user via OAuth token
func (s *DelegatedConfluenceSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, "", ErrAuthRequired
	}
	slogging.Get().Debug("DelegatedConfluenceSource: Fetch user=%s uri=%s", userID, uri)
	return s.Delegated.FetchForUser(ctx, userID, uri)
}

// ValidateAccess probes whether the user's token can read the page metadata
// without downloading the page body. Uses a per-call probe DelegatedSource
// to avoid racing against concurrent Fetch DoFetch invocations.
//
// Error semantics mirror DelegatedGoogleWorkspaceSource:
//   - (false, ErrAuthRequired): no user in context, no token, or failed_refresh.
//   - (false, ErrTransient): refresh hit a 5xx/network failure.
//   - (false, nil): page is not reachable for this user (4xx) or URL is
//     malformed; treated as "not accessible" rather than systemic.
//   - (true, nil): metadata probe succeeded.
//
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: validate that the user's OAuth token can access the given Confluence page URL
func (s *DelegatedConfluenceSource) ValidateAccess(ctx context.Context, uri string) (bool, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return false, ErrAuthRequired
	}
	var reachable bool
	probe := &DelegatedSource{
		ProviderID: s.Delegated.ProviderID,
		Tokens:     s.Delegated.Tokens,
		Registry:   s.Delegated.Registry,
		Skew:       s.Delegated.Skew,
		DoFetch: func(ctx context.Context, accessToken, probeURI string) ([]byte, string, error) {
			ok, err := s.probeMetadata(ctx, accessToken, probeURI)
			if err != nil {
				return nil, "", err
			}
			reachable = ok
			return nil, "", nil
		},
	}
	if _, _, err := probe.FetchForUser(ctx, userID, uri); err != nil {
		if errors.Is(err, ErrAuthRequired) || errors.Is(err, ErrTransient) {
			return false, err
		}
		return false, nil
	}
	return reachable, nil
}

// RequestAccess logs an actionable hint. Confluence has no programmatic
// access-request equivalent; the user-facing remediation is surfaced via
// access_diagnostics at the pipeline/handler level (re-link the account or
// ask a Confluence space admin for view permissions).
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: notify that Confluence access is unavailable; user must re-link (pure)
func (s *DelegatedConfluenceSource) RequestAccess(_ context.Context, uri string) error {
	slogging.Get().Info("DelegatedConfluenceSource: access not available for %s; user may need to re-link or request space access", uri)
	return nil
}

// doFetchPageView fetches the rendered HTML body of a Confluence Cloud page
// and returns it as text/html bytes. Steps:
//
//  1. Parse the URI to extract the host and page id.
//  2. Look up the cloud_id by calling /oauth/token/accessible-resources and
//     matching the URI host against each resource's url.
//  3. GET /ex/confluence/{cloud_id}/wiki/api/v2/pages/{id}?body-format=view.
//  4. Return body.view.value as bytes with content-type "text/html".
//
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: fetch the rendered HTML view body of a Confluence page via the v2 REST API
func (s *DelegatedConfluenceSource) doFetchPageView(ctx context.Context, accessToken, uri string) ([]byte, string, error) {
	host, pageID, err := parseConfluencePageURL(uri)
	if err != nil {
		return nil, "", err
	}
	cloudID, err := s.resolveCloudID(ctx, accessToken, host)
	if err != nil {
		return nil, "", err
	}
	endpoint := fmt.Sprintf("%s/ex/confluence/%s/wiki/api/v2/pages/%s?body-format=view",
		s.apiBase, url.PathEscape(cloudID), url.PathEscape(pageID))
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("Accept", "application/json")
	result, err := s.client.Fetch(ctx, endpoint, SafeFetchOptions{
		Method:       http.MethodGet,
		Headers:      headers,
		MaxBodyBytes: confluenceMaxBodySize,
	})
	if err != nil {
		return nil, "", fmt.Errorf("confluence: page request: %w", err)
	}
	if result.StatusCode != http.StatusOK {
		bodyPreview := result.Body
		if len(bodyPreview) > 1024 {
			bodyPreview = bodyPreview[:1024]
		}
		return nil, "", fmt.Errorf("confluence: page fetch status=%d body=%s", result.StatusCode, string(bodyPreview))
	}
	var payload struct {
		Body struct {
			View struct {
				Value string `json:"value"`
			} `json:"view"`
		} `json:"body"`
	}
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		return nil, "", fmt.Errorf("confluence: decode page body: %w", err)
	}
	if payload.Body.View.Value == "" {
		return nil, "", fmt.Errorf("confluence: page response has no body.view.value")
	}
	return []byte(payload.Body.View.Value), "text/html", nil
}

// probeMetadata issues a metadata-only GET against the page endpoint
// (no body-format) to determine accessibility without downloading the body.
// Returns (true, nil) on 200, (false, nil) on 4xx, error on 5xx/network/parse.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: validate that an OAuth token can reach a specific Confluence page (probe-only)
func (s *DelegatedConfluenceSource) probeMetadata(ctx context.Context, accessToken, uri string) (bool, error) {
	host, pageID, err := parseConfluencePageURL(uri)
	if err != nil {
		// Treat malformed URLs as "not accessible" rather than systemic
		// errors so ValidateAccess can return (false, nil).
		return false, fmt.Errorf("confluence probe: %w", err)
	}
	cloudID, err := s.resolveCloudID(ctx, accessToken, host)
	if err != nil {
		return false, err
	}
	endpoint := fmt.Sprintf("%s/ex/confluence/%s/wiki/api/v2/pages/%s",
		s.apiBase, url.PathEscape(cloudID), url.PathEscape(pageID))
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("Accept", "application/json")
	result, err := s.client.Fetch(ctx, endpoint, SafeFetchOptions{
		Method:  http.MethodGet,
		Headers: headers,
	})
	if err != nil {
		return false, fmt.Errorf("confluence probe: request: %w", err)
	}
	if result.StatusCode == http.StatusOK {
		return true, nil
	}
	if result.StatusCode >= 400 && result.StatusCode < 500 {
		return false, fmt.Errorf("confluence probe: status=%d", result.StatusCode)
	}
	return false, fmt.Errorf("confluence probe: status=%d", result.StatusCode)
}

// resolveCloudID looks up the Atlassian cloud_id for the given URI host by
// calling /oauth/token/accessible-resources and matching the host of each
// resource's url field. The bearer token is the user's Atlassian access
// token (scoped to read:confluence-content.all et al.).
//
// Returning an error (rather than e.g. returning the first resource) when
// the host does not match is intentional: we never want to fetch from a
// different tenant than the user requested.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: fetch the Atlassian cloud ID matching a given Confluence host from accessible resources
func (s *DelegatedConfluenceSource) resolveCloudID(ctx context.Context, accessToken, wantHost string) (string, error) {
	endpoint := s.apiBase + "/oauth/token/accessible-resources"
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("Accept", "application/json")
	result, err := s.client.Fetch(ctx, endpoint, SafeFetchOptions{
		Method:       http.MethodGet,
		Headers:      headers,
		MaxBodyBytes: confluenceMaxBodySize,
	})
	if err != nil {
		return "", fmt.Errorf("confluence accessible-resources: request: %w", err)
	}
	if result.StatusCode != http.StatusOK {
		return "", fmt.Errorf("confluence accessible-resources: status=%d body=%s",
			result.StatusCode, string(result.Body))
	}
	var resources []struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(result.Body, &resources); err != nil {
		return "", fmt.Errorf("confluence accessible-resources: decode: %w", err)
	}
	for _, r := range resources {
		host, ok := parseConfluenceHost(r.URL)
		if !ok {
			continue
		}
		if strings.EqualFold(host, wantHost) {
			return r.ID, nil
		}
	}
	return "", fmt.Errorf("confluence: no accessible resource matches host %s", wantHost)
}

// parseConfluencePageURL extracts the host and page id from a Confluence
// Cloud page URL of the form
// https://{host}/wiki/spaces/{SPACE}/pages/{id}[/{slug}].
//
// Legacy forms (/wiki/display/, /wiki/x/short links, REST URLs) and other
// non-page Confluence URLs are rejected — they return a clear error rather
// than triggering an extra round-trip to resolve them.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: parse a Confluence page URL into its host and numeric page ID (pure)
func parseConfluencePageURL(uri string) (host, pageID string, err error) {
	host, ok := parseConfluenceHost(uri)
	if !ok {
		return "", "", fmt.Errorf("confluence: invalid URL: %s", uri)
	}
	if !strings.HasSuffix(host, ".atlassian.net") {
		return "", "", fmt.Errorf("confluence: host %s is not an Atlassian Cloud host", host)
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", fmt.Errorf("confluence: parse URL: %w", err)
	}
	m := confluencePagePathRegex.FindStringSubmatch(parsed.Path)
	if len(m) < 2 {
		return "", "", fmt.Errorf("confluence: could not extract page id from path %s (legacy /display/ and /x/ forms are not supported)", parsed.Path)
	}
	return host, m[1], nil
}

// parseConfluenceHost returns the lowercased host of an http(s) URL, or
// ("", false) if the URL is not parseable or has no host.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: parse and normalize the hostname from a Confluence URL, stripping port (pure)
func parseConfluenceHost(uri string) (string, bool) {
	if uri == "" {
		return "", false
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(parsed.Host)
	if host == "" {
		return "", false
	}
	// Strip trailing port if any (api.atlassian.com style URLs don't have one,
	// but be defensive for site URLs returned by accessible-resources).
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host, true
}

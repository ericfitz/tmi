package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	// graphV1Base is the Microsoft Graph v1.0 root URL. Overridable in tests.
	graphV1Base = "https://graph.microsoft.com/v1.0"

	// microsoftMaxFetchSize caps the bytes read per Fetch to defend against
	// runaway downloads. Matches the Google Drive limit.
	microsoftMaxFetchSize = 10 * 1024 * 1024 // 10 MiB
)

// encodeMicrosoftShareID encodes a sharing URL as a Microsoft Graph share id
// suitable for GET /shares/{shareId}/driveItem.
//
// Per Microsoft Graph docs, the encoding is:
//  1. base64url-encode the URL
//  2. trim trailing "=" padding
//  3. prefix with "u!"
//
// This lets us address any SharePoint or OneDrive sharing URL — including
// URLs with embedded query strings, encoded paths, or short-link redirects —
// without parsing it manually.
func encodeMicrosoftShareID(uri string) string {
	b64 := base64.URLEncoding.EncodeToString([]byte(uri))
	b64 = strings.TrimRight(b64, "=")
	return "u!" + b64
}

// encodeMicrosoftPickerFileID encodes a (driveId, itemId) tuple into the
// existing picker_file_id column format. Microsoft Graph drive items are
// identified by both a drive id and item id (unlike Google Drive's single
// fileId), so we encode the tuple in the existing column rather than
// introducing a new schema field.
//
// Format: "{driveId}:{itemId}".
//
// Both values must be non-empty; the function does not validate input
// shape (Graph drive ids and item ids vary in syntax).
func encodeMicrosoftPickerFileID(driveID, itemID string) string {
	return driveID + ":" + itemID
}

// decodeMicrosoftPickerFileID splits a picker_file_id string back into
// (driveId, itemId). Returns ok=false when the input is missing the
// separator or either side is empty after splitting on the LAST colon.
//
// We split on the LAST colon because Microsoft drive ids may contain
// colons themselves (e.g. "b!Abc:def"). Item ids do not contain colons
// in any documented format.
func decodeMicrosoftPickerFileID(s string) (driveID, itemID string, ok bool) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return "", "", false
	}
	driveID = s[:idx]
	itemID = s[idx+1:]
	if driveID == "" || itemID == "" {
		return "", "", false
	}
	return driveID, itemID, true
}

// graphStatusError carries the HTTP status from Graph for error classification.
// 4xx (auth/notfound) indicates "not accessible"; 5xx indicates transient.
type graphStatusError struct {
	URL    string
	Status int
}

func (e *graphStatusError) Error() string {
	return fmt.Sprintf("graph %s: %d", e.URL, e.Status)
}

// isGraphTransient returns true when the underlying error is a graphStatusError
// with a 5xx status (or a network-level error that should be retried).
func isGraphTransient(err error) bool {
	var gse *graphStatusError
	if errors.As(err, &gse) {
		return gse.Status >= 500
	}
	// Network errors (DNS, connection refused, etc.) are transient.
	return err != nil && !errors.As(err, &gse)
}

// DelegatedMicrosoftSource fetches OneDrive-for-Business and SharePoint
// content under the user's own delegated identity. The user's token must
// carry Files.SelectedOperations.Selected (granted per-file by either the
// file owner — Experience 1, paste-URL with a copy-pasteable Graph snippet —
// or by TMI's picker-grant endpoint — Experience 2, after the user picks a
// file via the Microsoft File Picker). The provider name "microsoft" is
// reused for both OneDrive-for-Business and SharePoint Online. See issue
// #286 for the design discussion.
type DelegatedMicrosoftSource struct {
	// Delegated is the shared DelegatedSource helper.
	Delegated *DelegatedSource

	// GraphBaseURL overrides graphV1Base in tests.
	GraphBaseURL string

	// safeClient routes all Graph calls through SafeHTTPClient (scheme +
	// SSRF allowlist + DNS-pinning + body cap). Constructed via
	// NewDelegatedMicrosoftSource; tests may override via newMicrosoftSourceForTest.
	safeClient *SafeHTTPClient
}

// graphURL returns the configured Graph base or the default.
func (s *DelegatedMicrosoftSource) graphURL() string {
	if s.GraphBaseURL != "" {
		return s.GraphBaseURL
	}
	return graphV1Base
}

// Name returns the provider id "microsoft".
func (s *DelegatedMicrosoftSource) Name() string { return ProviderMicrosoft }

// CanHandle returns true for hosts served by the multi-audience Microsoft
// delegated provider:
//   - *.sharepoint.com       — Entra-managed OneDrive-for-Business + SharePoint (#286)
//   - onedrive.live.com      — consumer OneDrive root (#297)
//   - *.onedrive.live.com    — consumer OneDrive regional/tenant subdomains (#297)
//   - 1drv.ms                — consumer OneDrive short link (#297)
//
// All four route to the same DelegatedMicrosoftSource because Microsoft Graph
// /shares/{shareId}/driveItem resolves uniformly across audiences once the
// user has consented and per-file permission is in place.
func (s *DelegatedMicrosoftSource) CanHandle(_ context.Context, uri string) bool {
	if uri == "" {
		return false
	}
	host := extractHost(strings.ToLower(uri))
	switch {
	case strings.HasSuffix(host, microsoftHostSharePointSuffix):
		return true
	case host == microsoftHostOneDriveLive, strings.HasSuffix(host, "."+microsoftHostOneDriveLive):
		return true
	case host == microsoftHostOneDriveShort:
		return true
	}
	return false
}

// graphDriveItemMetadata is the subset of Graph's driveItem we need.
type graphDriveItemMetadata struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	File *struct {
		MimeType string `json:"mimeType"`
	} `json:"file,omitempty"`
}

// fetchByDriveItem fetches the content of (driveId, itemId) using the user's
// delegated bearer token. Reserved for the picker-metadata dispatch path
// (Task 10+); Fetch currently always uses fetchByURL since the
// Files.SelectedOperations.Selected grant from the picker makes
// /shares/{shareId}/driveItem work for picked files as well.
func (s *DelegatedMicrosoftSource) fetchByDriveItem(ctx context.Context, token, driveID, itemID string) ([]byte, string, error) {
	metaURL := fmt.Sprintf("%s/drives/%s/items/%s", s.graphURL(), driveID, itemID)
	meta, err := s.getDriveItemMetadata(ctx, token, metaURL)
	if err != nil {
		return nil, "", err
	}
	contentType := "application/octet-stream"
	if meta.File != nil && meta.File.MimeType != "" {
		contentType = meta.File.MimeType
	}

	contentURL := fmt.Sprintf("%s/drives/%s/items/%s/content", s.graphURL(), driveID, itemID)
	data, err := s.downloadFromGraph(ctx, token, contentURL)
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

func (s *DelegatedMicrosoftSource) getDriveItemMetadata(ctx context.Context, token, rawURL string) (*graphDriveItemMetadata, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	result, err := s.safeClient.Fetch(ctx, rawURL, SafeFetchOptions{
		Method:  http.MethodGet,
		Headers: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("graph metadata: %w", err)
	}
	if result.StatusCode >= 400 {
		bodyPreview := result.Body
		if len(bodyPreview) > 1024 {
			bodyPreview = bodyPreview[:1024]
		}
		if len(bodyPreview) > 0 {
			slogging.Get().Debug("graph error response url=%s status=%d body=%s", rawURL, result.StatusCode, string(bodyPreview))
		}
		return nil, &graphStatusError{URL: rawURL, Status: result.StatusCode}
	}
	var meta graphDriveItemMetadata
	if err := json.Unmarshal(result.Body, &meta); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	return &meta, nil
}

func (s *DelegatedMicrosoftSource) downloadFromGraph(ctx context.Context, token, rawURL string) ([]byte, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	result, err := s.safeClient.Fetch(ctx, rawURL, SafeFetchOptions{
		Method:       http.MethodGet,
		Headers:      headers,
		MaxBodyBytes: microsoftMaxFetchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("graph download: %w", err)
	}
	if result.StatusCode >= 400 {
		bodyPreview := result.Body
		if len(bodyPreview) > 1024 {
			bodyPreview = bodyPreview[:1024]
		}
		if len(bodyPreview) > 0 {
			slogging.Get().Debug("graph error response url=%s status=%d body=%s", rawURL, result.StatusCode, string(bodyPreview))
		}
		return nil, &graphStatusError{URL: rawURL, Status: result.StatusCode}
	}
	return result.Body, nil
}

// fetchByURL resolves a SharePoint URL to a drive item via /shares/{shareId}/driveItem
// and downloads the content. Used when no picker metadata is present on the
// document (Experience 1 — paste-URL flow), and also when picker metadata is
// present, since the Files.SelectedOperations.Selected grant from the picker
// makes /shares/{shareId}/driveItem succeed for that specific file.
func (s *DelegatedMicrosoftSource) fetchByURL(ctx context.Context, token, uri string) ([]byte, string, error) {
	shareID := encodeMicrosoftShareID(uri)
	metaURL := fmt.Sprintf("%s/shares/%s/driveItem", s.graphURL(), shareID)
	meta, err := s.getDriveItemMetadata(ctx, token, metaURL)
	if err != nil {
		return nil, "", fmt.Errorf("resolve share id: %w", err)
	}
	contentURL := fmt.Sprintf("%s/shares/%s/driveItem/content", s.graphURL(), shareID)
	data, err := s.downloadFromGraph(ctx, token, contentURL)
	if err != nil {
		return nil, "", err
	}
	contentType := "application/octet-stream"
	if meta.File != nil && meta.File.MimeType != "" {
		contentType = meta.File.MimeType
	}
	return data, contentType, nil
}

// NewDelegatedMicrosoftSource constructs a source wired to the given token
// repository and OAuth provider registry. The DoFetch callback uses the
// share-id resolution path (Experience 1). The picker-mediated path is
// equivalent at fetch time because the per-file grant from the picker
// makes /shares/{shareId}/driveItem succeed for that specific file.
//
// validator MUST be non-nil; in production it is built from the operator's
// content-source allowlist (typically containing graph.microsoft.com).
func NewDelegatedMicrosoftSource(
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
	validator *URIValidator,
) *DelegatedMicrosoftSource {
	source := &DelegatedMicrosoftSource{
		safeClient: NewSafeHTTPClient(
			validator,
			WithDefaultTimeouts(30*time.Second, 10*time.Second, microsoftMaxFetchSize),
		),
	}
	doFetch := func(ctx context.Context, token, uri string) ([]byte, string, error) {
		slogging.Get().Debug("DelegatedMicrosoftSource: fetch uri=%s", uri)
		return source.fetchByURL(ctx, token, uri)
	}
	source.Delegated = &DelegatedSource{
		ProviderID: ProviderMicrosoft,
		Tokens:     tokens,
		Registry:   registry,
		DoFetch:    doFetch,
	}
	return source
}

// Fetch returns the raw bytes of the referenced SharePoint/OneDrive-for-Business
// file for the user in ctx. Requires UserIDFromContext to return a non-empty
// user id; delegated sources cannot run without user context.
func (s *DelegatedMicrosoftSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, "", ErrAuthRequired
	}
	return s.Delegated.FetchForUser(ctx, userID, uri)
}

// ValidateAccess checks whether the user's token can resolve the URI to a
// drive item without downloading the body. Per-call probe DelegatedSource
// avoids racing on the shared source's DoFetch field when concurrent
// ValidateAccess calls are in flight.
//
// Error semantics:
//   - (false, ErrAuthRequired): no user, no token, or token in failed_refresh.
//   - (false, ErrTransient): provider returned 5xx during refresh OR Graph 5xx.
//   - (false, nil): Graph returned 4xx (not accessible).
//   - (true, nil): metadata probe succeeded.
func (s *DelegatedMicrosoftSource) ValidateAccess(ctx context.Context, uri string) (bool, error) {
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
		DoFetch: func(ctx context.Context, token, uri string) ([]byte, string, error) {
			shareID := encodeMicrosoftShareID(uri)
			metaURL := fmt.Sprintf("%s/shares/%s/driveItem", s.graphURL(), shareID)
			if _, err := s.getDriveItemMetadata(ctx, token, metaURL); err != nil {
				return nil, "", err
			}
			reachable = true
			return nil, "", nil
		},
	}
	if _, _, err := probe.FetchForUser(ctx, userID, uri); err != nil {
		if errors.Is(err, ErrAuthRequired) {
			return false, err
		}
		// Graph 5xx → transient.
		if isGraphTransient(err) {
			return false, ErrTransient
		}
		// Wrapped 5xx from DelegatedSource.refresh path also returns ErrTransient.
		if errors.Is(err, ErrTransient) {
			return false, err
		}
		// Anything else (Graph 4xx, malformed URI) → not accessible, no error.
		return false, nil
	}
	return reachable, nil
}

// RequestAccess logs an informational entry and returns nil. The user-facing
// remediation is surfaced via document access_diagnostics (reason_code +
// remediations[]) at the handler level. See api/access_diagnostics.go and the
// document GET handler for the user-visible "share with TMI app" snippet.
func (s *DelegatedMicrosoftSource) RequestAccess(_ context.Context, uri string) error {
	slogging.Get().Info("DelegatedMicrosoftSource: access not yet granted for %s; user must share the file with the TMI app via the per-file Graph permissions API", uri)
	return nil
}

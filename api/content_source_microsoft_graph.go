package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
// file owner — Experience 1 — or by TMI's picker-grant endpoint —
// Experience 2). See specs/2026-04-26-microsoft-delegated-provider-design.md.
type DelegatedMicrosoftSource struct {
	// Delegated is the shared DelegatedSource helper.
	Delegated *DelegatedSource

	// GraphBaseURL overrides graphV1Base in tests.
	GraphBaseURL string

	// httpClient is used for all Graph HTTP calls; defaults to a 30-second
	// timeout client. Tests may override to use a server's client.
	httpClient *http.Client
}

// graphURL returns the configured Graph base or the default.
func (s *DelegatedMicrosoftSource) graphURL() string {
	if s.GraphBaseURL != "" {
		return s.GraphBaseURL
	}
	return graphV1Base
}

// client returns the HTTP client to use for Graph calls. Defaults to a new
// client with a 30-second timeout when httpClient is nil, so that direct
// struct construction in tests (without explicit timeout setup) still works.
func (s *DelegatedMicrosoftSource) client() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Name returns the provider id "microsoft".
func (s *DelegatedMicrosoftSource) Name() string { return ProviderMicrosoft }

// CanHandle returns true for *.sharepoint.com hosts (covers OneDrive-for-Business
// at *-my.sharepoint.com and any SharePoint site). Personal Microsoft account
// hosts (onedrive.live.com, 1drv.ms) are deliberately not handled here; they
// will be picked up by a future personal-account sub-project.
func (s *DelegatedMicrosoftSource) CanHandle(_ context.Context, uri string) bool {
	if uri == "" {
		return false
	}
	host := extractHost(strings.ToLower(uri))
	return strings.HasSuffix(host, ".sharepoint.com")
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

func (s *DelegatedMicrosoftSource) getDriveItemMetadata(ctx context.Context, token, url string) (*graphDriveItemMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build metadata request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.client().Do(req) //nolint:gosec // G107 - Graph URL constructed from validated drive/item ids
	if err != nil {
		return nil, fmt.Errorf("graph metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if len(body) > 0 {
			slogging.Get().Debug("graph error response url=%s status=%d body=%s", url, resp.StatusCode, string(body))
		}
		return nil, &graphStatusError{URL: url, Status: resp.StatusCode}
	}
	var meta graphDriveItemMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	return &meta, nil
}

func (s *DelegatedMicrosoftSource) downloadFromGraph(ctx context.Context, token, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.client().Do(req) //nolint:gosec // G107 - Graph URL constructed from validated drive/item ids
	if err != nil {
		return nil, fmt.Errorf("graph download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if len(body) > 0 {
			slogging.Get().Debug("graph error response url=%s status=%d body=%s", url, resp.StatusCode, string(body))
		}
		return nil, &graphStatusError{URL: url, Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, microsoftMaxFetchSize))
	if err != nil {
		return nil, fmt.Errorf("read download: %w", err)
	}
	return data, nil
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
func NewDelegatedMicrosoftSource(
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
) *DelegatedMicrosoftSource {
	source := &DelegatedMicrosoftSource{
		httpClient: &http.Client{Timeout: 30 * time.Second},
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

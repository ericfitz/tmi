package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G107 - Graph URL constructed from validated drive/item ids
	if err != nil {
		return nil, fmt.Errorf("graph metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("graph %s: %d", url, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("graph %s: %d %s", url, resp.StatusCode, string(body))
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
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G107 - Graph URL constructed from validated drive/item ids
	if err != nil {
		return nil, fmt.Errorf("graph download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("graph download %s: %d %s", url, resp.StatusCode, string(body))
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
	source := &DelegatedMicrosoftSource{}
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

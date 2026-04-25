package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// DelegatedGoogleWorkspaceSource fetches Drive documents under the user's own
// identity via drive.file-scoped tokens granted through Google Picker. Documents
// must have been picker-registered (stored picker_file_id) for fetches to
// succeed; picker registration is performed by the client at attach time.
//
// The dispatch layer (ContentSourceRegistry.FindSourceForDocument, added in
// Task 4.1) decides whether to route this source or fall through to the
// service-account GoogleDriveSource based on whether the document has
// picker metadata and the user has a linked token.
//
// Construct via NewDelegatedGoogleWorkspaceSource (Task 3.2). A zero-value
// struct has no Delegated helper and will panic on Fetch.
type DelegatedGoogleWorkspaceSource struct {
	// Delegated is the shared DelegatedSource helper (Task 3.2 sets DoFetch).
	Delegated *DelegatedSource

	// PickerDeveloperKey and PickerAppID are returned to clients via the
	// picker-token endpoint (Task 5.2); they are not used by Fetch itself.
	PickerDeveloperKey string
	PickerAppID        string
}

// exportFormatFor returns the MIME type to request when exporting a Google
// Workspace native document, or "" for binary files that should be downloaded
// directly. Matches the behavior of the service-account GoogleDriveSource.
func exportFormatFor(mime string) string {
	switch mime {
	case "application/vnd.google-apps.document":
		return "text/plain"
	case "application/vnd.google-apps.spreadsheet":
		return "text/csv"
	case "application/vnd.google-apps.presentation":
		return "text/plain"
	default:
		return ""
	}
}

// newDriveService builds a Google Drive v3 client using the given bearer
// token. The client is lightweight (single HTTP transport) and safe to
// construct per call; callers pass a short-lived token that DelegatedSource
// has just refreshed.
func newDriveService(ctx context.Context, accessToken string) (*drive.Service, error) {
	return drive.NewService(ctx, option.WithTokenSource(staticTokenSource(accessToken)))
}

// staticTokenSource wraps a bearer access token as an oauth2.TokenSource for
// use with the Google Drive API client. The token is short-lived (the helper
// layer refreshes before handing it to DoFetch), so no reuse across calls.
func staticTokenSource(token string) oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
}

// NewDelegatedGoogleWorkspaceSource constructs a source wired to the given
// token repository and OAuth provider registry. The DoFetch callback creates
// a Drive service with the supplied access token on each call (no connection
// caching — lightweight at this scale).
func NewDelegatedGoogleWorkspaceSource(
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
	pickerDeveloperKey, pickerAppID string,
) *DelegatedGoogleWorkspaceSource {
	doFetch := func(ctx context.Context, accessToken, uri string) ([]byte, string, error) {
		fileID, ok := extractGoogleDriveFileID(uri)
		if !ok {
			return nil, "", fmt.Errorf("could not extract file ID from URL: %s", uri)
		}
		svc, err := newDriveService(ctx, accessToken)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create Drive service: %w", err)
		}
		file, err := svc.Files.Get(fileID).Fields("id,name,mimeType").Context(ctx).Do()
		if err != nil {
			return nil, "", fmt.Errorf("failed to get file metadata: %w", err)
		}
		exportMime := exportFormatFor(file.MimeType)
		if exportMime != "" {
			resp, err := svc.Files.Export(fileID, exportMime).Context(ctx).Download()
			if err != nil {
				return nil, "", fmt.Errorf("failed to export file: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()
			data, err := io.ReadAll(io.LimitReader(resp.Body, googleDriveMaxExportSize))
			if err != nil {
				return nil, "", fmt.Errorf("failed to read export: %w", err)
			}
			return data, exportMime, nil
		}
		resp, err := svc.Files.Get(fileID).Context(ctx).Download()
		if err != nil {
			return nil, "", fmt.Errorf("failed to download file: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(io.LimitReader(resp.Body, googleDriveMaxExportSize))
		if err != nil {
			return nil, "", fmt.Errorf("failed to read download: %w", err)
		}
		return data, file.MimeType, nil
	}
	return &DelegatedGoogleWorkspaceSource{
		Delegated: &DelegatedSource{
			ProviderID: ProviderGoogleWorkspace,
			Tokens:     tokens,
			Registry:   registry,
			DoFetch:    doFetch,
		},
		PickerDeveloperKey: pickerDeveloperKey,
		PickerAppID:        pickerAppID,
	}
}

// ValidateAccess checks whether the user's token can see the referenced file
// without downloading content. Implementation uses a per-call probe
// DelegatedSource so concurrent ValidateAccess calls don't race on the
// shared source's DoFetch field.
//
// Error semantics:
//   - (false, ErrAuthRequired): no user in context, no linked token, or the
//     stored token is in failed_refresh.
//   - (false, ErrTransient): provider returned 5xx/network during refresh.
//   - (false, nil): Drive returned a 4xx for the file (not accessible); this
//     includes malformed file id extraction, since that is treated as
//     "we can't reach this file" rather than a systemic error.
//   - (true, nil): Drive accepted the metadata probe.
func (s *DelegatedGoogleWorkspaceSource) ValidateAccess(ctx context.Context, uri string) (bool, error) {
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
		DoFetch: func(ctx context.Context, accessToken, uri string) ([]byte, string, error) {
			fileID, extracted := extractGoogleDriveFileID(uri)
			if !extracted {
				return nil, "", fmt.Errorf("could not extract file ID from URL: %s", uri)
			}
			svc, err := newDriveService(ctx, accessToken)
			if err != nil {
				return nil, "", err
			}
			if _, err := svc.Files.Get(fileID).Fields("id").Context(ctx).Do(); err != nil {
				return nil, "", err
			}
			reachable = true
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

// RequestAccess logs an actionable hint. The actual user-facing remediation
// is surfaced via access_diagnostics (reason_code + remediations[]) at the
// pipeline/handler level. This method exists to satisfy the AccessRequester
// interface; no Drive API call is made.
func (s *DelegatedGoogleWorkspaceSource) RequestAccess(_ context.Context, uri string) error {
	slogging.Get().Info("DelegatedGoogleWorkspaceSource: access not available for %s; user may need to re-link or repick", uri)
	return nil
}

// Name returns the provider id "google_workspace".
func (s *DelegatedGoogleWorkspaceSource) Name() string { return ProviderGoogleWorkspace }

// CanHandle returns true for Google Docs and Google Drive URIs. The dispatch
// layer (FindSourceForDocument) is responsible for deciding whether this
// source or the service-account GoogleDriveSource handles a given document;
// CanHandle only filters by URI host.
func (s *DelegatedGoogleWorkspaceSource) CanHandle(_ context.Context, uri string) bool {
	lower := strings.ToLower(uri)
	host := extractHost(lower)
	return host == googleHostDocs || host == googleHostDrive
}

// Fetch returns the raw bytes of the referenced Drive file for the user in
// ctx. Requires UserIDFromContext to return a non-empty user id; delegated
// sources cannot run without user context.
//
// The actual Drive API call lives in the DelegatedSource.DoFetch callback
// (Task 3.2 implementation); this skeleton delegates to the helper.
func (s *DelegatedGoogleWorkspaceSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return nil, "", ErrAuthRequired
	}
	slogging.Get().Debug("DelegatedGoogleWorkspaceSource: Fetch user=%s uri=%s", userID, uri)
	return s.Delegated.FetchForUser(ctx, userID, uri)
}

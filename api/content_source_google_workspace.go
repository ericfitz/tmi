package api

import (
	"context"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
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

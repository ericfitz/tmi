package api

import "context"

// ContentTokenLinkedChecker implements LinkedProviderChecker over a
// ContentTokenRepository. It returns true when the user has a token for the
// given provider with status == ContentTokenStatusActive.
//
// When tokens is nil or any input is empty, returns false (closed-fail).
type ContentTokenLinkedChecker struct {
	tokens ContentTokenRepository
}

// NewContentTokenLinkedChecker constructs a LinkedProviderChecker backed by
// the given ContentTokenRepository.
func NewContentTokenLinkedChecker(tokens ContentTokenRepository) *ContentTokenLinkedChecker {
	return &ContentTokenLinkedChecker{tokens: tokens}
}

// HasActiveToken returns true iff the user has an active linked token for
// the provider. See LinkedProviderChecker.
func (c *ContentTokenLinkedChecker) HasActiveToken(ctx context.Context, userID, providerID string) bool {
	if c == nil || c.tokens == nil || userID == "" || providerID == "" {
		return false
	}
	tok, err := c.tokens.GetByUserAndProvider(ctx, userID, providerID)
	if err != nil || tok == nil {
		return false
	}
	return tok.Status == ContentTokenStatusActive
}

// PickerMetadata carries the picker-registration fields from a document row.
// When present, it indicates the client attached the document via Google
// Picker (or equivalent), and the FindSourceForDocument dispatch may route
// to the delegated source if the user has an active linked token.
//
// All three fields are non-nil together or all nil together (invariant
// enforced at attach time).
type PickerMetadata struct {
	ProviderID *string
	FileID     *string
	MimeType   *string
}

// LinkedProviderChecker reports whether a user has an active (non-failed)
// linked token for a given provider. Implementations typically consult the
// ContentTokenRepository.
type LinkedProviderChecker interface {
	HasActiveToken(ctx context.Context, userID, providerID string) bool
}

// FindSourceForDocument picks a ContentSource for fetching a specific
// document. The delegated source wins when the document has picker metadata
// for a provider that is registered and the user has an active linked token
// for that provider. Otherwise, dispatch falls through to URL-based lookup
// (which picks the first CanHandle match in registration order).
//
// Why not put this on ContentSource or Document: URL-based dispatch must
// still work for documents that predate the picker feature, and picker
// metadata is a per-row concern. Keeping the dispatch logic here
// centralizes the policy.
func (r *ContentSourceRegistry) FindSourceForDocument(
	ctx context.Context,
	uri string,
	picker *PickerMetadata,
	userID string,
	checker LinkedProviderChecker,
) (ContentSource, bool) {
	if picker != nil && picker.ProviderID != nil && *picker.ProviderID != "" && checker != nil {
		providerID := *picker.ProviderID
		if src, found := r.FindSourceByName(providerID); found {
			if checker.HasActiveToken(ctx, userID, providerID) {
				return src, true
			}
		}
	}
	return r.FindSource(ctx, uri)
}

package api

import "context"

// userIDContextKey is the context key for the authenticated user's identifier.
// SEM@910d076563691e5e679e89d83c82fdca8d04f2b3: context key type for the authenticated user identifier (pure)
type userIDContextKey struct{}

// UserIDFromContext reads the user identifier set by JWT middleware or tests.
// Returns ("", false) when no user ID is present or it is empty.
// SEM@910d076563691e5e679e89d83c82fdca8d04f2b3: fetch the authenticated user identifier from a context (pure)
func UserIDFromContext(ctx context.Context) (string, bool) {
	v := ctx.Value(userIDContextKey{})
	s, ok := v.(string)
	return s, ok && s != ""
}

// WithUserID returns a new context carrying the given user ID.
// Used by middleware and tests to attach a user id to request contexts.
// SEM@910d076563691e5e679e89d83c82fdca8d04f2b3: build a context carrying a user identifier (pure)
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

// ContentSource authenticates and fetches raw bytes from a URI.
// SEM@789146ae6555f1667678ed835a68faac5b22ad30: interface for authenticating and fetching raw bytes from a URI
type ContentSource interface {
	Name() string
	CanHandle(ctx context.Context, uri string) bool
	Fetch(ctx context.Context, uri string) (data []byte, contentType string, err error)
}

// AccessValidator checks whether a source can access a URI without downloading it.
// SEM@789146ae6555f1667678ed835a68faac5b22ad30: interface for probing URI accessibility without downloading content
type AccessValidator interface {
	ValidateAccess(ctx context.Context, uri string) (accessible bool, err error)
}

// AccessRequester programmatically requests access to a URI (e.g., share request email).
// SEM@789146ae6555f1667678ed835a68faac5b22ad30: interface for programmatically requesting access to a URI
type AccessRequester interface {
	RequestAccess(ctx context.Context, uri string) error
}

// ContentSourceRegistry manages content sources in priority order.
// SEM@789146ae6555f1667678ed835a68faac5b22ad30: ordered registry of content sources for URI dispatch (pure)
type ContentSourceRegistry struct {
	sources []ContentSource
}

// NewContentSourceRegistry creates a new registry.
// SEM@789146ae6555f1667678ed835a68faac5b22ad30: build an empty content source registry (pure)
func NewContentSourceRegistry() *ContentSourceRegistry {
	return &ContentSourceRegistry{}
}

// Register adds a source to the registry (tried in registration order).
// SEM@789146ae6555f1667678ed835a68faac5b22ad30: add a content source to the registry in priority order (mutates shared state)
func (r *ContentSourceRegistry) Register(source ContentSource) {
	r.sources = append(r.sources, source)
}

// FindSource returns the first source that can handle the given URI.
// SEM@789146ae6555f1667678ed835a68faac5b22ad30: find the first registered content source that can handle a URI (pure)
func (r *ContentSourceRegistry) FindSource(ctx context.Context, uri string) (ContentSource, bool) {
	for _, s := range r.sources {
		if s.CanHandle(ctx, uri) {
			return s, true
		}
	}
	return nil, false
}

// FindSourceByName returns the source with the given name, if registered.
// SEM@90539292d25d541a7e322a67f50ecb928268f215: find a registered content source by provider name (pure)
func (r *ContentSourceRegistry) FindSourceByName(name string) (ContentSource, bool) {
	for _, s := range r.sources {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}

// Names returns the names of all registered sources.
// SEM@cccf2de369faf0f0361b1d829e82b8ce594f4a99: list names of all registered content sources in priority order (pure)
func (r *ContentSourceRegistry) Names() []string {
	names := make([]string, len(r.sources))
	for i, s := range r.sources {
		names[i] = s.Name()
	}
	return names
}

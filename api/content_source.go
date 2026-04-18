package api

import "context"

// userIDContextKey is the context key for the authenticated user's identifier.
type userIDContextKey struct{}

// UserIDFromContext reads the user identifier set by JWT middleware or tests.
// Returns ("", false) when no user ID is present or it is empty.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v := ctx.Value(userIDContextKey{})
	s, ok := v.(string)
	return s, ok && s != ""
}

// WithUserID returns a new context carrying the given user ID.
// Used by middleware and tests to attach a user id to request contexts.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

// ContentSource authenticates and fetches raw bytes from a URI.
type ContentSource interface {
	Name() string
	CanHandle(ctx context.Context, uri string) bool
	Fetch(ctx context.Context, uri string) (data []byte, contentType string, err error)
}

// AccessValidator checks whether a source can access a URI without downloading it.
type AccessValidator interface {
	ValidateAccess(ctx context.Context, uri string) (accessible bool, err error)
}

// AccessRequester programmatically requests access to a URI (e.g., share request email).
type AccessRequester interface {
	RequestAccess(ctx context.Context, uri string) error
}

// ContentSourceRegistry manages content sources in priority order.
type ContentSourceRegistry struct {
	sources []ContentSource
}

// NewContentSourceRegistry creates a new registry.
func NewContentSourceRegistry() *ContentSourceRegistry {
	return &ContentSourceRegistry{}
}

// Register adds a source to the registry (tried in registration order).
func (r *ContentSourceRegistry) Register(source ContentSource) {
	r.sources = append(r.sources, source)
}

// FindSource returns the first source that can handle the given URI.
func (r *ContentSourceRegistry) FindSource(ctx context.Context, uri string) (ContentSource, bool) {
	for _, s := range r.sources {
		if s.CanHandle(ctx, uri) {
			return s, true
		}
	}
	return nil, false
}

// FindSourceByName returns the source with the given name, if registered.
func (r *ContentSourceRegistry) FindSourceByName(name string) (ContentSource, bool) {
	for _, s := range r.sources {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}

// Names returns the names of all registered sources.
func (r *ContentSourceRegistry) Names() []string {
	names := make([]string, len(r.sources))
	for i, s := range r.sources {
		names[i] = s.Name()
	}
	return names
}

package auth

import "context"

// dbLoginResolver is the resolver interactive login (OAuth and SAML) uses for
// tiered user matching. It embeds *Service but serves the three lookups that
// Service normally answers from the Redis user cache straight from the
// database.
//
// Login decides whether a user row must be created, so it cannot trust the
// cache: an entry can outlive its row (a database restore or reset while
// Redis keeps its keys). Matching on such an entry skipped the create and
// minted a token for a user that did not exist, which then broke every write
// keyed on the user, such as first-user auto-promotion (#1162). Each lookup
// still refreshes the cache from what it read, so the per-request paths that
// do use the cache see the current row after the next login.
type dbLoginResolver struct {
	*Service
}

// loginResolver returns the database-authoritative resolver for login.
func (s *Service) loginResolver() samlUserResolver {
	return dbLoginResolver{Service: s}
}

func (r dbLoginResolver) GetUserByProviderID(ctx context.Context, provider, providerUserID string) (User, error) {
	return r.lookupUserByProviderID(ctx, provider, providerUserID)
}

func (r dbLoginResolver) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return r.lookupUserByEmail(ctx, email)
}

func (r dbLoginResolver) GetUserByInternalUUID(ctx context.Context, uuid string) (User, error) {
	return r.lookupUserByID(ctx, uuid)
}

package api

import (
	"context"

	"github.com/google/uuid"
)

// DelegationTokenIssuer is the seam between the api package and the auth
// package's IssueAddonDelegationToken. The webhook delivery worker uses it
// to mint a fresh, scoped delegation JWT for each addon.invoked delivery so
// the downstream addon can perform write-backs as the invoker (T18, #358)
// rather than as its own service-account.
//
// Set from cmd/server/main.go after the auth service is initialized via
// SetGlobalDelegationTokenIssuer. The implementation lives in
// auth_service_adapter.go and wraps auth.Service.GetUserByID +
// auth.Service.IssueAddonDelegationToken.
type DelegationTokenIssuer interface {
	// IssueForInvocation mints a delegation JWT impersonating the invoker
	// (looked up by invokerInternalUUID) for one addon-invocation
	// write-back. The token's scope is bound to addonID + deliveryID +
	// threatModelID, with a wall-clock TTL matching the addon-invocation
	// budget (60s today). On success it returns the encoded JWT.
	IssueForInvocation(
		ctx context.Context,
		invokerInternalUUID string,
		addonID, deliveryID, threatModelID uuid.UUID,
	) (string, error)
}

// GlobalDelegationTokenIssuer is the package-level hook the webhook
// delivery worker reads. nil during early startup and in unit tests that
// do not exercise the addon path; the worker degrades gracefully (no
// delegation token attached) when nil and logs at Warn.
var GlobalDelegationTokenIssuer DelegationTokenIssuer

// SetGlobalDelegationTokenIssuer sets the global delegation-token issuer.
// Safe to call once at startup from cmd/server/main.go.
func SetGlobalDelegationTokenIssuer(issuer DelegationTokenIssuer) {
	GlobalDelegationTokenIssuer = issuer
}

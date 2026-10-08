// Package errcode holds the closed vocabulary of machine-readable error codes
// returned in the "error" field (and "details.code") of TMI API error bodies
// (ADR 2026-10-08 error-code vocabulary, issue #1048). It is dependency-free
// so that both the api and auth packages can share it without an import cycle
// (auth must not import api).
package errcode

// Code is a machine-readable error code.
type Code string

// Tier 1: REST codes (Error.error enum in the OpenAPI spec).
const (
	// InvalidInput is HTTP 400 body, field, header or query value fails validation.
	InvalidInput Code = "invalid_input"
	// InvalidID is HTTP 400 a path or query identifier is malformed.
	InvalidID Code = "invalid_id"
	// InvalidPatch is HTTP 400 JSON Patch document malformed, disallowed or inapplicable.
	InvalidPatch Code = "invalid_patch"
	// Unauthorized is HTTP 401 missing, expired or invalid credentials.
	Unauthorized Code = "unauthorized"
	// InsufficientUserAuthentication is HTTP 401 step-up authentication required (RFC 9470).
	InsufficientUserAuthentication Code = "insufficient_user_authentication"
	// Forbidden is HTTP 403 authenticated but not permitted.
	Forbidden Code = "forbidden"
	// NotFound is HTTP 404 resource or route does not exist or is hidden by authorization.
	NotFound Code = "not_found"
	// MethodNotAllowed is HTTP 405 method not allowed.
	MethodNotAllowed Code = "method_not_allowed"
	// NotAcceptable is HTTP 406 not acceptable.
	NotAcceptable Code = "not_acceptable"
	// Conflict is HTTP 409 state conflict: duplicate, in use, wrong lifecycle state.
	Conflict Code = "conflict"
	// Gone is HTTP 410 permanently removed.
	Gone Code = "gone"
	// VersionMismatch is HTTP 412 If-Match does not match the current version.
	VersionMismatch Code = "version_mismatch"
	// PayloadTooLarge is HTTP 413 payload too large.
	PayloadTooLarge Code = "payload_too_large"
	// UnsupportedMediaType is HTTP 415 Content-Type not accepted.
	UnsupportedMediaType Code = "unsupported_media_type"
	// UnprocessableEntity is HTTP 422 well-formed request that cannot be processed in the current state.
	UnprocessableEntity Code = "unprocessable_entity"
	// IfMatchRequired is HTTP 428 If-Match header missing.
	IfMatchRequired Code = "if_match_required"
	// RateLimitExceeded is HTTP 429 transient limit; honor Retry-After.
	RateLimitExceeded Code = "rate_limit_exceeded"
	// QuotaExceeded is HTTP 403 or 429 hard cap reached; retrying does not help.
	QuotaExceeded Code = "quota_exceeded"
	// ServerError is HTTP 500 unexpected failure, including panic recovery.
	ServerError Code = "server_error"
	// NotImplemented is HTTP 501 operation not supported by this server.
	NotImplemented Code = "not_implemented"
	// ServiceUnavailable is HTTP 503 dependency unavailable; retry later.
	ServiceUnavailable Code = "service_unavailable"
)

// Tier 1b: additional codes accepted on protocol routes (OAuthError.error enum).
// ServerError and InsufficientUserAuthentication above are shared with this tier.
const (
	// InvalidRequest: RFC 6749 malformed protocol request.
	InvalidRequest Code = "invalid_request"
	// InvalidClient: RFC 6749 client authentication failed.
	InvalidClient Code = "invalid_client"
	// InvalidGrant: RFC 6749 grant invalid, expired or revoked.
	InvalidGrant Code = "invalid_grant"
	// UnauthorizedClient: RFC 6749 client not authorized for this grant type.
	UnauthorizedClient Code = "unauthorized_client"
	// UnsupportedGrantType: RFC 6749 grant type not supported.
	UnsupportedGrantType Code = "unsupported_grant_type"
	// InvalidScope: RFC 6749 requested scope invalid.
	InvalidScope Code = "invalid_scope"
	// AccessDenied: RFC 6749 resource owner or server denied the request.
	AccessDenied Code = "access_denied"
	// UnsupportedResponseType: RFC 6749 response type not supported.
	UnsupportedResponseType Code = "unsupported_response_type"
	// TemporarilyUnavailable: RFC 6749 server temporarily unable to handle the request.
	TemporarilyUnavailable Code = "temporarily_unavailable"
	// UnsupportedTokenType: RFC 7009 token type not supported for revocation.
	UnsupportedTokenType Code = "unsupported_token_type"
	// IdentityMismatch: TMI extension: provider identity does not match the expected user.
	IdentityMismatch Code = "identity_mismatch"
	// AccountConflict: TMI extension: account conflicts with an existing account.
	AccountConflict Code = "account_conflict"
	// EmailNotVerified: TMI extension: provider email is not verified.
	EmailNotVerified Code = "email_not_verified"
	// ProviderUnreachable: TMI extension: identity provider could not be reached.
	ProviderUnreachable Code = "provider_unreachable"
	// ProviderResponseInvalid: TMI extension: identity provider returned an invalid response.
	ProviderResponseInvalid Code = "provider_response_invalid"
	// InvalidProvider: TMI extension: identity provider is unknown or disabled.
	InvalidProvider Code = "invalid_provider"
)

// Tier 2: domain reasons carried in details.code. The list is open; adding a
// reason does not require a schema version bump.
const (
	DetailPickerFileIDMismatch              Code = "picker_file_id_mismatch"
	DetailInvalidPickerRegistration         Code = "invalid_picker_registration"
	DetailInvalidChallenge                  Code = "invalid_challenge"
	DetailInvalidProviderType               Code = "invalid_provider_type"
	DetailClientCallbackRequired            Code = "client_callback_required"
	DetailClientCallbackNotAllowed          Code = "client_callback_not_allowed"
	DetailInvalidIfMatch                    Code = "invalid_if_match"
	DetailInvalidVersion                    Code = "invalid_version"
	DetailDuplicateHeader                   Code = "duplicate_header"
	DetailUnsupportedEncoding               Code = "unsupported_encoding"
	DetailReservedKey                       Code = "reserved_key"
	DetailTokenNotLinkedOrFailed            Code = "token_not_linked_or_failed"
	DetailInvalidToken                      Code = "invalid_token"
	DetailProtectedGroup                    Code = "protected_group"
	DetailProviderMismatch                  Code = "provider_mismatch"
	DetailSessionNotFound                   Code = "session_not_found"
	DetailFeatureNotAvailable               Code = "feature_not_available"
	DetailDuplicateGroup                    Code = "duplicate_group"
	DetailDuplicateMembership               Code = "duplicate_membership"
	DetailSelfDeletion                      Code = "self_deletion"
	DetailProtectedUser                     Code = "protected_user"
	DetailDeletionBlocked                   Code = "deletion_blocked"
	DetailEncryptionNotEnabled              Code = "encryption_not_enabled"
	DetailUnreadableSettingsLimit           Code = "unreadable_settings_limit"
	DetailSessionNotActive                  Code = "session_not_active"
	DetailProviderNotRegistered             Code = "provider_not_registered"
	DetailProviderNotConfigured             Code = "provider_not_configured"
	DetailContentTokenProviderNotConfigured Code = "content_token_provider_not_configured"
	DetailDimensionMismatch                 Code = "dimension_mismatch"
	DetailInconsistentDimensions            Code = "inconsistent_dimensions"
	DetailNoSource                          Code = "no_source"
	DetailAccessRequestNotSupported         Code = "access_request_not_supported"
	DetailTooManyConnections                Code = "too_many_connections"
	DetailSessionFull                       Code = "session_full"
	DetailMessageRateLimit                  Code = "message_rate_limit"
	DetailDuplicateInvocation               Code = "duplicate_invocation"
	DetailSessionLimitExceeded              Code = "session_limit_exceeded"
	DetailWebsocketUpgradeFailed            Code = "websocket_upgrade_failed"
	DetailLlmBusy                           Code = "llm_busy"
	DetailLlmNotConfigured                  Code = "llm_not_configured"
	DetailCollaborationSessionExists        Code = "collaboration_session_exists"
)

// transportCodes are emitted by route-agnostic middleware before dispatch, so
// they are valid on protocol routes too.
var transportCodes = []Code{Unauthorized, NotFound, MethodNotAllowed, NotAcceptable, RateLimitExceeded, ServerError}

// rfcCodes lists the RFC 6749/7009/9470 codes plus the documented TMI extensions.
var rfcCodes = []Code{
	InvalidRequest, InvalidClient, InvalidGrant, UnauthorizedClient, UnsupportedGrantType,
	InvalidScope, AccessDenied, UnsupportedResponseType, ServerError, TemporarilyUnavailable,
	UnsupportedTokenType, InsufficientUserAuthentication,
	IdentityMismatch, AccountConflict, EmailNotVerified, ProviderUnreachable,
	ProviderResponseInvalid, InvalidProvider,
}

var restCodes = []Code{
	InvalidInput, InvalidID, InvalidPatch, Unauthorized, InsufficientUserAuthentication, Forbidden, NotFound, MethodNotAllowed, NotAcceptable, Conflict, Gone, VersionMismatch, PayloadTooLarge, UnsupportedMediaType, UnprocessableEntity, IfMatchRequired, RateLimitExceeded, QuotaExceeded, ServerError, NotImplemented, ServiceUnavailable,
}

var detailCodes = []Code{
	DetailPickerFileIDMismatch,
	DetailInvalidPickerRegistration,
	DetailInvalidChallenge,
	DetailInvalidProviderType,
	DetailClientCallbackRequired,
	DetailClientCallbackNotAllowed,
	DetailInvalidIfMatch,
	DetailInvalidVersion,
	DetailDuplicateHeader,
	DetailUnsupportedEncoding,
	DetailReservedKey,
	DetailTokenNotLinkedOrFailed,
	DetailInvalidToken,
	DetailProtectedGroup,
	DetailProviderMismatch,
	DetailSessionNotFound,
	DetailFeatureNotAvailable,
	DetailDuplicateGroup,
	DetailDuplicateMembership,
	DetailSelfDeletion,
	DetailProtectedUser,
	DetailDeletionBlocked,
	DetailEncryptionNotEnabled,
	DetailUnreadableSettingsLimit,
	DetailSessionNotActive,
	DetailProviderNotRegistered,
	DetailProviderNotConfigured,
	DetailContentTokenProviderNotConfigured,
	DetailDimensionMismatch,
	DetailInconsistentDimensions,
	DetailNoSource,
	DetailAccessRequestNotSupported,
	DetailTooManyConnections,
	DetailSessionFull,
	DetailMessageRateLimit,
	DetailDuplicateInvocation,
	DetailSessionLimitExceeded,
	DetailWebsocketUpgradeFailed,
	DetailLlmBusy,
	DetailLlmNotConfigured,
	DetailCollaborationSessionExists,
}

var (
	restSet     = toSet(restCodes)
	protocolSet = toSet(protocolCodes())
)

func protocolCodes() []Code {
	out := append([]Code{}, rfcCodes...)
	for _, c := range transportCodes {
		if !contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

func contains(s []Code, c Code) bool {
	for _, x := range s {
		if x == c {
			return true
		}
	}
	return false
}

func toSet(s []Code) map[Code]struct{} {
	m := make(map[Code]struct{}, len(s))
	for _, c := range s {
		m[c] = struct{}{}
	}
	return m
}

// REST returns the Tier 1 codes documented as the Error.error enum.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: list the REST error codes (pure)
func REST() []Code { return append([]Code{}, restCodes...) }

// Protocol returns the codes documented as the OAuthError.error enum.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: list the protocol-route error codes (pure)
func Protocol() []Code { return protocolCodes() }

// Details returns the known details.code reasons.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: list the known details.code reasons (pure)
func Details() []Code { return append([]Code{}, detailCodes...) }

// IsREST reports whether c is a documented REST error code.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: check membership in the REST error vocabulary (pure)
func IsREST(c Code) bool { _, ok := restSet[c]; return ok }

// IsProtocol reports whether c is a documented protocol-route error code.
// SEM@d5bdfb1ec1b8a5b6ae052d7475c567f2499f9824: check membership in the protocol error vocabulary (pure)
func IsProtocol(c Code) bool { _, ok := protocolSet[c]; return ok }

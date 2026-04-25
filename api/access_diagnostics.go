package api

// Reason codes for DocumentAccessDiagnostics.reason_code (stable API contract).
// tmi-ux localizes messages by these codes.
const (
	ReasonTokenNotLinked            = "token_not_linked"
	ReasonTokenRefreshFailed        = "token_refresh_failed"
	ReasonTokenTransientFailure     = "token_transient_failure"
	ReasonPickerRegistrationInvalid = "picker_registration_invalid"
	ReasonNoAccessibleSource        = "no_accessible_source"
	ReasonSourceNotFound            = "source_not_found"
	ReasonFetchError                = "fetch_error"
	ReasonOther                     = "other"
)

// Remediation actions for DocumentAccessDiagnostics.remediations[].action.
const (
	RemediationLinkAccount             = "link_account"
	RemediationRelinkAccount           = "relink_account"
	RemediationRepickFile              = "repick_file"
	RemediationShareWithServiceAccount = "share_with_service_account"
	RemediationRepickAfterShare        = "repick_after_share"
	RemediationRetry                   = "retry"
	RemediationContactOwner            = "contact_owner"
)

// BuilderContext carries everything the builder needs to assemble a diagnostics
// object. Empty fields are treated as "not applicable" — the builder tolerates
// missing context.
type BuilderContext struct {
	ReasonCode   string
	ReasonDetail string

	// ProviderID is the provider relevant to the error (e.g. "google_workspace"),
	// used to populate remediation params.
	ProviderID string

	// Caller context (the user viewing the document, not necessarily the owner).
	CallerUserEmail       string
	CallerLinkedProviders map[string]bool // provider_id -> has-active-token

	// Config-sourced values for specific remediations.
	ServiceAccountEmail string

	// Owner context (optional; used for contact_owner remediation).
	DocumentOwnerEmail string
}

// AccessRemediationDiag is the builder's internal representation of a single
// remediation. The API-wire type is regenerated from OpenAPI in Phase 8; this
// internal type is converted at the handler boundary.
type AccessRemediationDiag struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params"`
}

// AccessDiagnosticsDiag is the builder's internal representation of
// access_diagnostics. See AccessRemediationDiag.
type AccessDiagnosticsDiag struct {
	ReasonCode   string                  `json:"reason_code"`
	ReasonDetail *string                 `json:"reason_detail,omitempty"`
	Remediations []AccessRemediationDiag `json:"remediations"`
}

// BuildAccessDiagnostics returns a diagnostic object given the builder context,
// or nil when there is no diagnostic to report (empty ReasonCode).
func BuildAccessDiagnostics(ctx BuilderContext) *AccessDiagnosticsDiag {
	if ctx.ReasonCode == "" {
		return nil
	}
	d := &AccessDiagnosticsDiag{
		ReasonCode:   ctx.ReasonCode,
		Remediations: []AccessRemediationDiag{},
	}
	if ctx.ReasonCode == ReasonOther && ctx.ReasonDetail != "" {
		det := ctx.ReasonDetail
		d.ReasonDetail = &det
	}

	switch ctx.ReasonCode {
	case ReasonTokenNotLinked:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationLinkAccount,
			Params: map[string]interface{}{"provider_id": ctx.ProviderID},
		})
	case ReasonTokenRefreshFailed:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationRelinkAccount,
			Params: map[string]interface{}{"provider_id": ctx.ProviderID},
		})
	case ReasonTokenTransientFailure, ReasonFetchError:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationRetry,
			Params: map[string]interface{}{},
		})
	case ReasonPickerRegistrationInvalid:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationRepickFile,
			Params: map[string]interface{}{"provider_id": ctx.ProviderID},
		})
	case ReasonNoAccessibleSource:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationShareWithServiceAccount,
			Params: map[string]interface{}{"service_account_email": ctx.ServiceAccountEmail},
		})
		if ctx.CallerLinkedProviders[ProviderGoogleWorkspace] {
			d.Remediations = append(d.Remediations, AccessRemediationDiag{
				Action: RemediationRepickAfterShare,
				Params: map[string]interface{}{
					"provider_id": ProviderGoogleWorkspace,
					"user_email":  ctx.CallerUserEmail,
				},
			})
		}
	case ReasonSourceNotFound, ReasonOther:
		// No remediations.
	}
	return d
}

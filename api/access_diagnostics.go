package api

import "fmt"

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
	// ReasonMicrosoftNotShared is emitted when a SharePoint/OneDrive document
	// has not been shared with the TMI Entra application. The remediation
	// provides a copy-pasteable Graph API call the file owner can run to grant
	// per-file read access.
	ReasonMicrosoftNotShared = "microsoft_not_shared"
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
	// RemediationShareWithApplication is emitted for the microsoft_not_shared
	// reason. Params include drive_id, item_id, app_object_id, graph_call, and
	// graph_body so the file owner can paste the Graph snippet directly.
	RemediationShareWithApplication = "share_with_application"
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

	// MicrosoftDriveID, MicrosoftItemID, and MicrosoftApplicationObjectID are
	// populated for the share_with_application remediation. The handler
	// resolves drive/item ids from picker metadata or the Graph /shares/{id}
	// lookup done during ValidateAccess; the application object id is the
	// TMI Entra app's object id, configured at startup.
	MicrosoftDriveID             string
	MicrosoftItemID              string
	MicrosoftApplicationObjectID string
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
	case ReasonMicrosoftNotShared:
		// Use placeholder text in the copy-pasteable graph_call snippet when
		// drive/item IDs are not yet resolved (paste-URL degraded state). The
		// actual drive_id/item_id params reflect the truthful (possibly empty)
		// values from ctx so the caller can detect the degraded state.
		driveID := ctx.MicrosoftDriveID
		if driveID == "" {
			driveID = "{driveId}"
		}
		itemID := ctx.MicrosoftItemID
		if itemID == "" {
			itemID = "{itemId}"
		}
		graphCall := fmt.Sprintf(
			"POST https://graph.microsoft.com/v1.0/drives/%s/items/%s/permissions",
			driveID, itemID)
		graphBody := fmt.Sprintf(
			`{"roles":["read"],"grantedToIdentities":[{"application":{"id":"%s","displayName":"TMI"}}]}`,
			ctx.MicrosoftApplicationObjectID)
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationShareWithApplication,
			Params: map[string]interface{}{
				"drive_id":      ctx.MicrosoftDriveID,
				"item_id":       ctx.MicrosoftItemID,
				"app_object_id": ctx.MicrosoftApplicationObjectID,
				"graph_call":    graphCall,
				"graph_body":    graphBody,
			},
		})
	case ReasonSourceNotFound, ReasonOther:
		// No remediations.
	}
	return d
}

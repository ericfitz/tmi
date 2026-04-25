package api

import (
	"testing"
)

func TestDiagnosticConstants(t *testing.T) {
	cases := []string{
		ReasonTokenNotLinked,
		ReasonTokenRefreshFailed,
		ReasonTokenTransientFailure,
		ReasonPickerRegistrationInvalid,
		ReasonNoAccessibleSource,
		ReasonSourceNotFound,
		ReasonFetchError,
		ReasonOther,
	}
	for _, c := range cases {
		if c == "" {
			t.Error("reason code constant is empty")
		}
	}
	actions := []string{
		RemediationLinkAccount,
		RemediationRelinkAccount,
		RemediationRepickFile,
		RemediationShareWithServiceAccount,
		RemediationRepickAfterShare,
		RemediationRetry,
		RemediationContactOwner,
	}
	for _, a := range actions {
		if a == "" {
			t.Error("remediation action constant is empty")
		}
	}
}

func TestBuildAccessDiagnostics_NoReason(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{
		ReasonCode:          "",
		CallerUserEmail:     "alice@example.com",
		ServiceAccountEmail: "indexer@tmi.iam.gserviceaccount.com",
	})
	if d != nil {
		t.Fatalf("expected nil diagnostics when reason_code is empty, got %+v", d)
	}
}

func TestBuildAccessDiagnostics_TokenNotLinked(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{
		ReasonCode: ReasonTokenNotLinked,
		ProviderID: ProviderGoogleWorkspace,
	})
	if d == nil {
		t.Fatal("expected non-nil diagnostics")
	}
	if d.ReasonCode != ReasonTokenNotLinked {
		t.Fatalf("reason_code: got %q, want %q", d.ReasonCode, ReasonTokenNotLinked)
	}
	if len(d.Remediations) != 1 {
		t.Fatalf("expected 1 remediation, got %d: %+v", len(d.Remediations), d.Remediations)
	}
	if d.Remediations[0].Action != RemediationLinkAccount {
		t.Fatalf("action: got %q, want %q", d.Remediations[0].Action, RemediationLinkAccount)
	}
	if d.Remediations[0].Params["provider_id"] != ProviderGoogleWorkspace {
		t.Fatalf("missing provider_id param: %+v", d.Remediations[0].Params)
	}
}

func TestBuildAccessDiagnostics_TokenRefreshFailed(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{
		ReasonCode: ReasonTokenRefreshFailed,
		ProviderID: ProviderGoogleWorkspace,
	})
	if d.Remediations[0].Action != RemediationRelinkAccount {
		t.Fatalf("expected relink_account, got %s", d.Remediations[0].Action)
	}
	if d.Remediations[0].Params["provider_id"] != ProviderGoogleWorkspace {
		t.Fatalf("missing provider_id param")
	}
}

func TestBuildAccessDiagnostics_TransientFailure(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{ReasonCode: ReasonTokenTransientFailure})
	if d.Remediations[0].Action != RemediationRetry {
		t.Fatalf("expected retry, got %s", d.Remediations[0].Action)
	}
}

func TestBuildAccessDiagnostics_PickerInvalid(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{
		ReasonCode: ReasonPickerRegistrationInvalid,
		ProviderID: ProviderGoogleWorkspace,
	})
	if d.Remediations[0].Action != RemediationRepickFile {
		t.Fatalf("expected repick_file, got %s", d.Remediations[0].Action)
	}
}

func TestBuildAccessDiagnostics_NoAccessibleSource_Unlinked(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{
		ReasonCode:          ReasonNoAccessibleSource,
		ServiceAccountEmail: "indexer@tmi.iam.gserviceaccount.com",
	})
	if len(d.Remediations) != 1 {
		t.Fatalf("expected 1 remediation, got %d", len(d.Remediations))
	}
	if d.Remediations[0].Action != RemediationShareWithServiceAccount {
		t.Fatalf("expected share_with_service_account, got %s", d.Remediations[0].Action)
	}
	if d.Remediations[0].Params["service_account_email"] != "indexer@tmi.iam.gserviceaccount.com" {
		t.Fatalf("missing service_account_email param")
	}
}

func TestBuildAccessDiagnostics_NoAccessibleSource_Linked(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{
		ReasonCode:            ReasonNoAccessibleSource,
		ServiceAccountEmail:   "indexer@tmi.iam.gserviceaccount.com",
		CallerUserEmail:       "alice@example.com",
		CallerLinkedProviders: map[string]bool{ProviderGoogleWorkspace: true},
	})
	if len(d.Remediations) != 2 {
		t.Fatalf("expected 2 remediations, got %d: %+v", len(d.Remediations), d.Remediations)
	}
	if d.Remediations[0].Action != RemediationShareWithServiceAccount {
		t.Fatalf("remediation[0]: got %s, want share_with_service_account", d.Remediations[0].Action)
	}
	if d.Remediations[1].Action != RemediationRepickAfterShare {
		t.Fatalf("remediation[1]: got %s, want repick_after_share", d.Remediations[1].Action)
	}
	if d.Remediations[1].Params["user_email"] != "alice@example.com" {
		t.Fatalf("missing user_email param: %+v", d.Remediations[1].Params)
	}
	if d.Remediations[1].Params["provider_id"] != ProviderGoogleWorkspace {
		t.Fatalf("missing provider_id param: %+v", d.Remediations[1].Params)
	}
}

func TestBuildAccessDiagnostics_SourceNotFound(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{ReasonCode: ReasonSourceNotFound})
	if len(d.Remediations) != 0 {
		t.Fatalf("expected 0 remediations, got %+v", d.Remediations)
	}
}

func TestBuildAccessDiagnostics_FetchError(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{ReasonCode: ReasonFetchError})
	if d.Remediations[0].Action != RemediationRetry {
		t.Fatalf("expected retry, got %s", d.Remediations[0].Action)
	}
}

func TestBuildAccessDiagnostics_Other_IncludesDetail(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{
		ReasonCode:   ReasonOther,
		ReasonDetail: "drive quota exceeded",
	})
	if d.ReasonDetail == nil || *d.ReasonDetail != "drive quota exceeded" {
		t.Fatalf("expected reason_detail passthrough, got %v", d.ReasonDetail)
	}
	if len(d.Remediations) != 0 {
		t.Fatalf("expected empty remediations for 'other', got %+v", d.Remediations)
	}
}

func TestBuildAccessDiagnostics_Other_NoDetail(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{ReasonCode: ReasonOther})
	if d.ReasonDetail != nil {
		t.Fatalf("expected nil reason_detail when not provided, got %v", *d.ReasonDetail)
	}
}

package api

import "testing"

func TestLookupContentProviderMeta_KnownIDs(t *testing.T) {
	cases := []struct {
		id   string
		kind ContentProviderKind
		name string
		icon string
	}{
		{"http", ContentProviderKindDirect, "HTTP", "fa-solid fa-globe"},
		{"google_drive", ContentProviderKindService, "Google Drive", "fa-brands fa-google-drive"},
		{"google_workspace", ContentProviderKindDelegated, "Google Workspace", "fa-brands fa-google"},
		{"microsoft", ContentProviderKindDelegated, "Microsoft 365", "fa-brands fa-microsoft"},
		{"confluence", ContentProviderKindDelegated, "Atlassian Confluence", "fa-brands fa-confluence"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			m := lookupContentProviderMeta(tc.id)
			if m.Kind != tc.kind {
				t.Errorf("Kind = %q, want %q", m.Kind, tc.kind)
			}
			if m.DefaultName != tc.name {
				t.Errorf("DefaultName = %q, want %q", m.DefaultName, tc.name)
			}
			if m.DefaultIcon != tc.icon {
				t.Errorf("DefaultIcon = %q, want %q", m.DefaultIcon, tc.icon)
			}
		})
	}
}

func TestLookupContentProviderMeta_UnknownID(t *testing.T) {
	m := lookupContentProviderMeta("experimental")
	if m.Kind != ContentProviderKindDirect {
		t.Errorf("Kind = %q, want %q", m.Kind, ContentProviderKindDirect)
	}
	if m.DefaultName != "experimental" {
		t.Errorf("DefaultName = %q, want %q", m.DefaultName, "experimental")
	}
	if m.DefaultIcon != "" {
		t.Errorf("DefaultIcon = %q, want empty", m.DefaultIcon)
	}
}

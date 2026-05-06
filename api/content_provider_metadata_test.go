package api

import "testing"

func TestLookupContentProviderMeta_KnownIDs(t *testing.T) {
	cases := []struct {
		id   string
		kind string
		name string
		icon string
	}{
		{"http", "direct", "HTTP", "fa-solid fa-globe"},
		{"google_drive", "service", "Google Drive", "fa-brands fa-google-drive"},
		{"google_workspace", "delegated", "Google Workspace", "fa-brands fa-google"},
		{"microsoft", "delegated", "Microsoft 365", "fa-brands fa-microsoft"},
		{"confluence", "delegated", "Atlassian Confluence", "fa-brands fa-confluence"},
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
	if m.Kind != "direct" {
		t.Errorf("Kind = %q, want %q", m.Kind, "direct")
	}
	if m.DefaultName != "experimental" {
		t.Errorf("DefaultName = %q, want %q", m.DefaultName, "experimental")
	}
	if m.DefaultIcon != "" {
		t.Errorf("DefaultIcon = %q, want empty", m.DefaultIcon)
	}
}

package api

import "testing"

func TestRedactPendingLinkPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "link pending path with token is redacted",
			input: "/me/identities/link/pending/abc123xyz-secret-token-here",
			want:  "/me/identities/link/pending/(redacted)",
		},
		{
			name:  "prefix only (no token) is redacted",
			input: "/me/identities/link/pending/",
			want:  "/me/identities/link/pending/(redacted)",
		},
		{
			name:  "other me/identities paths are unchanged",
			input: "/me/identities",
			want:  "/me/identities",
		},
		{
			name:  "link start path is unchanged",
			input: "/me/identities/link/start",
			want:  "/me/identities/link/start",
		},
		{
			name:  "link confirm path is unchanged",
			input: "/me/identities/link/confirm",
			want:  "/me/identities/link/confirm",
		},
		{
			name:  "unrelated path is unchanged",
			input: "/threat_models/1234",
			want:  "/threat_models/1234",
		},
		{
			name:  "empty path is unchanged",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactPendingLinkPath(tt.input)
			if got != tt.want {
				t.Errorf("redactPendingLinkPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

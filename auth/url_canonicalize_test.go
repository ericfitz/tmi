package auth

import (
	"testing"
)

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "trailing slash on multi-segment path is stripped",
			input: "https://example.com/userinfo/",
			want:  "https://example.com/userinfo",
		},
		{
			name:  "trailing slash on root path is preserved",
			input: "https://example.com/",
			want:  "https://example.com/",
		},
		{
			name:  "default https port is stripped",
			input: "https://example.com:443/userinfo",
			want:  "https://example.com/userinfo",
		},
		{
			name:  "default http port is stripped",
			input: "http://example.com:80/userinfo",
			want:  "http://example.com/userinfo",
		},
		{
			name:  "non-default port is preserved",
			input: "https://example.com:8443/userinfo",
			want:  "https://example.com:8443/userinfo",
		},
		{
			name:  "uppercase scheme and host are lowercased",
			input: "HTTPS://EXAMPLE.COM/userinfo",
			want:  "https://example.com/userinfo",
		},
		{
			name:  "already canonical URL is unchanged",
			input: "https://openidconnect.googleapis.com/v1/userinfo",
			want:  "https://openidconnect.googleapis.com/v1/userinfo",
		},
		{
			name:  "empty string returns input unchanged",
			input: "",
			want:  "",
		},
		{
			name:  "URL without host returns input unchanged",
			input: "not-a-url",
			want:  "not-a-url",
		},
		{
			name:  "no path change when path is exactly root",
			input: "https://example.com",
			want:  "https://example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("canonicalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

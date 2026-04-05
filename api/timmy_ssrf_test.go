package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSSRFValidator_BlocksPrivateIPs(t *testing.T) {
	v := NewSSRFValidator(nil)
	tests := []struct {
		name string
		url  string
	}{
		{"RFC1918 10.x", "http://10.0.0.1/doc.pdf"},
		{"RFC1918 172.16.x", "http://172.16.0.1/doc.pdf"},
		{"RFC1918 192.168.x", "http://192.168.1.1/doc.pdf"},
		{"Loopback", "http://127.0.0.1/doc.pdf"},
		{"Loopback localhost", "http://localhost/doc.pdf"},
		{"Link-local", "http://169.254.169.254/latest/meta-data/"},
		{"IPv6 loopback", "http://[::1]/doc.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.url)
			assert.Error(t, err, "should block %s", tt.url)
		})
	}
}

func TestSSRFValidator_AllowsPublicURLs(t *testing.T) {
	v := NewSSRFValidator(nil)
	tests := []struct {
		name string
		url  string
	}{
		{"Public HTTP", "http://example.com/doc.pdf"},
		{"Public HTTPS", "https://docs.google.com/document/d/123"},
		{"Public IP", "http://8.8.8.8/page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.url)
			assert.NoError(t, err, "should allow %s", tt.url)
		})
	}
}

func TestSSRFValidator_Allowlist(t *testing.T) {
	v := NewSSRFValidator([]string{"internal.corp.com", "wiki.internal.net"})
	err := v.Validate("https://internal.corp.com/page")
	assert.NoError(t, err, "allowlisted host should be allowed")

	err = v.Validate("http://10.0.0.1/page")
	assert.Error(t, err, "non-allowlisted private IP should still be blocked")
}

func TestSSRFValidator_RejectsNonHTTP(t *testing.T) {
	v := NewSSRFValidator(nil)
	tests := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://evil.com",
	}
	for _, url := range tests {
		err := v.Validate(url)
		assert.Error(t, err, "should reject non-HTTP scheme: %s", url)
	}
}

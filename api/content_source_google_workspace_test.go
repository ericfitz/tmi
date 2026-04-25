package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDelegatedGoogleWorkspaceSource_ExportFormatFor(t *testing.T) {
	cases := []struct {
		mime           string
		expectedFormat string
	}{
		{"application/vnd.google-apps.document", "text/plain"},
		{"application/vnd.google-apps.spreadsheet", "text/csv"},
		{"application/vnd.google-apps.presentation", "text/plain"},
		{"application/pdf", ""},
		{"image/png", ""},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.mime, func(t *testing.T) {
			assert.Equal(t, c.expectedFormat, exportFormatFor(c.mime))
		})
	}
}

func TestDelegatedGoogleWorkspaceSource_Name(t *testing.T) {
	s := &DelegatedGoogleWorkspaceSource{}
	assert.Equal(t, ProviderGoogleWorkspace, s.Name())
	assert.Equal(t, "google_workspace", s.Name())
}

func TestDelegatedGoogleWorkspaceSource_CanHandle(t *testing.T) {
	s := &DelegatedGoogleWorkspaceSource{}
	cases := []struct {
		uri string
		ok  bool
	}{
		{"https://docs.google.com/document/d/abc/edit", true},
		{"https://drive.google.com/file/d/abc/view", true},
		{"https://docs.google.com/spreadsheets/d/xyz/edit", true},
		{"https://docs.google.com/presentation/d/pqr/edit", true},
		{"https://drive.google.com/open?id=abc", true},
		{"https://example.com/doc", false},
		{"https://confluence.example.com/wiki/page", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.uri, func(t *testing.T) {
			assert.Equal(t, c.ok, s.CanHandle(context.Background(), c.uri))
		})
	}
}

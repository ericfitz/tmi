package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGoogleDriveSource_CanHandle(t *testing.T) {
	s := &GoogleDriveSource{}

	tests := []struct {
		uri      string
		expected bool
	}{
		{"https://docs.google.com/document/d/abc123/edit", true},
		{"https://docs.google.com/spreadsheets/d/abc123/edit", true},
		{"https://docs.google.com/presentation/d/abc123/edit", true},
		{"https://drive.google.com/file/d/abc123/view", true},
		{"https://drive.google.com/open?id=abc123", true},
		{"https://example.com/doc.html", false},
		{"https://confluence.example.com/wiki/page", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandle(context.Background(), tt.uri))
		})
	}
}

func TestGoogleDriveSource_ExtractFileID(t *testing.T) {
	tests := []struct {
		uri    string
		fileID string
		ok     bool
	}{
		{"https://docs.google.com/document/d/1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgVE2upms/edit", "1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgVE2upms", true},
		{"https://docs.google.com/spreadsheets/d/abc123/edit#gid=0", "abc123", true},
		{"https://drive.google.com/file/d/abc123/view", "abc123", true},
		{"https://drive.google.com/open?id=abc123", "abc123", true},
		{"https://example.com/doc", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			fileID, ok := extractGoogleDriveFileID(tt.uri)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.fileID, fileID)
			}
		})
	}
}

func TestGoogleDriveSource_Name(t *testing.T) {
	s := &GoogleDriveSource{}
	assert.Equal(t, "google_drive", s.Name())
}

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeMicrosoftShareID(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		// Examples from Microsoft Graph documentation.
		{"https://onedrive.live.com/redir?resid=1234", "u!aHR0cHM6Ly9vbmVkcml2ZS5saXZlLmNvbS9yZWRpcj9yZXNpZD0xMjM0"},
		{"https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/Doc.docx",
			"u!aHR0cHM6Ly9jb250b3NvLnNoYXJlcG9pbnQuY29tL3NpdGVzL01hcmtldGluZy9TaGFyZWQlMjBEb2N1bWVudHMvRG9jLmRvY3g"},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			assert.Equal(t, tc.want, encodeMicrosoftShareID(tc.uri))
		})
	}
}

func TestEncodeDecodeMicrosoftPickerFileID(t *testing.T) {
	cases := []struct {
		name    string
		driveID string
		itemID  string
		encoded string
		ok      bool
	}{
		{name: "round-trip", driveID: "b!abc", itemID: "01XYZ", encoded: "b!abc:01XYZ", ok: true},
		{name: "with colons in driveID", driveID: "b!a:b:c", itemID: "01XYZ", encoded: "b!a:b:c:01XYZ", ok: true},
		{name: "empty drive (decode)", driveID: "", itemID: "01XYZ", encoded: ":01XYZ", ok: false},
		{name: "empty item (decode)", driveID: "b!abc", itemID: "", encoded: "b!abc:", ok: false},
		{name: "no separator", driveID: "", itemID: "", encoded: "noseparator", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.driveID != "" && tc.itemID != "" {
				assert.Equal(t, tc.encoded, encodeMicrosoftPickerFileID(tc.driveID, tc.itemID))
			}
			driveID, itemID, ok := decodeMicrosoftPickerFileID(tc.encoded)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.Equal(t, tc.driveID, driveID)
				assert.Equal(t, tc.itemID, itemID)
			}
		})
	}
}

func TestDelegatedMicrosoftSource_Name(t *testing.T) {
	s := &DelegatedMicrosoftSource{}
	assert.Equal(t, "microsoft", s.Name())
}

func TestDelegatedMicrosoftSource_CanHandle(t *testing.T) {
	s := &DelegatedMicrosoftSource{}
	cases := []struct {
		uri      string
		expected bool
	}{
		{"https://contoso.sharepoint.com/sites/Marketing/Doc.docx", true},
		{"https://contoso-my.sharepoint.com/personal/alice/Documents/draft.pptx", true},
		{"https://onedrive.live.com/redir?resid=1234", false}, // personal — out of scope
		{"https://1drv.ms/abc", false},                        // personal short link — out of scope
		{"https://docs.google.com/document/d/abc/edit", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			assert.Equal(t, tc.expected, s.CanHandle(context.Background(), tc.uri))
		})
	}
}

func TestDelegatedMicrosoftSource_FetchByDriveItem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drives/b!abc/items/01XYZ":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"01XYZ","name":"hello.txt","file":{"mimeType":"text/plain"}}`))
		case "/drives/b!abc/items/01XYZ/content":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("hello"))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	s := &DelegatedMicrosoftSource{GraphBaseURL: server.URL}
	data, contentType, err := s.fetchByDriveItem(context.Background(), "test-token", "b!abc", "01XYZ")
	require.NoError(t, err)
	assert.Equal(t, "text/plain", contentType)
	assert.Equal(t, []byte("hello"), data)
}

func TestNewDelegatedMicrosoftSource_BasicConstruction(t *testing.T) {
	s := NewDelegatedMicrosoftSource(nil, nil)
	require.NotNil(t, s)
	require.NotNil(t, s.Delegated)
	assert.Equal(t, ProviderMicrosoft, s.Delegated.ProviderID)
	require.NotNil(t, s.httpClient)
}

func TestDelegatedMicrosoftSource_ValidateAccess_403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	s := &DelegatedMicrosoftSource{GraphBaseURL: server.URL, httpClient: server.Client()}
	_, err := s.getDriveItemMetadata(context.Background(), "tok",
		fmt.Sprintf("%s/shares/u!abc/driveItem", server.URL))
	require.Error(t, err)
	var gse *graphStatusError
	require.True(t, errors.As(err, &gse))
	assert.Equal(t, http.StatusForbidden, gse.Status)
	assert.False(t, isGraphTransient(err))
}

func TestDelegatedMicrosoftSource_ValidateAccess_503Transient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	s := &DelegatedMicrosoftSource{GraphBaseURL: server.URL, httpClient: server.Client()}
	_, err := s.getDriveItemMetadata(context.Background(), "tok",
		fmt.Sprintf("%s/shares/u!abc/driveItem", server.URL))
	require.Error(t, err)
	assert.True(t, isGraphTransient(err))
}

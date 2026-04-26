package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
			assert.Equal(t, tc.want, EncodeMicrosoftShareID(tc.uri))
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
				assert.Equal(t, tc.encoded, EncodeMicrosoftPickerFileID(tc.driveID, tc.itemID))
			}
			driveID, itemID, ok := DecodeMicrosoftPickerFileID(tc.encoded)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.Equal(t, tc.driveID, driveID)
				assert.Equal(t, tc.itemID, itemID)
			}
		})
	}
}

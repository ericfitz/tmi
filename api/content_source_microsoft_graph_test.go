package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

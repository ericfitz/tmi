package api

import "strings"

// EncodeMicrosoftPickerFileID encodes a (driveId, itemId) tuple into the
// existing picker_file_id column format. Microsoft Graph drive items are
// identified by both a drive id and item id (unlike Google Drive's single
// fileId), so we encode the tuple in the existing column rather than
// introducing a new schema field.
//
// Format: "{driveId}:{itemId}".
//
// Both values must be non-empty; the function does not validate input
// shape (Graph drive ids and item ids vary in syntax).
func EncodeMicrosoftPickerFileID(driveID, itemID string) string {
	return driveID + ":" + itemID
}

// DecodeMicrosoftPickerFileID splits a picker_file_id string back into
// (driveId, itemId). Returns ok=false when the input is missing the
// separator or either side is empty after splitting on the LAST colon.
//
// We split on the LAST colon because Microsoft drive ids may contain
// colons themselves (e.g. "b!Abc:def"). Item ids do not contain colons
// in any documented format.
func DecodeMicrosoftPickerFileID(s string) (driveID, itemID string, ok bool) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return "", "", false
	}
	driveID = s[:idx]
	itemID = s[idx+1:]
	if driveID == "" || itemID == "" {
		return "", "", false
	}
	return driveID, itemID, true
}

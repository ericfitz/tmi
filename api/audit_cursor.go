package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// auditCursor is the keyset-pagination position for audit list endpoints:
// the (created_at, id) of the last row returned. Encoded opaque so clients
// cannot depend on its structure (#398).
type auditCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"i"`
}

func encodeAuditCursor(createdAt time.Time, id string) string {
	b, _ := json.Marshal(auditCursor{CreatedAt: createdAt.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeAuditCursor(s string) (*auditCursor, error) {
	if s == "" {
		return nil, fmt.Errorf("empty cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	var c auditCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("invalid cursor payload: %w", err)
	}
	if c.CreatedAt.IsZero() || c.ID == "" {
		return nil, fmt.Errorf("incomplete cursor")
	}
	return &c, nil
}

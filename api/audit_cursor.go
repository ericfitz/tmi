package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// auditCursor is the keyset-pagination position for audit list endpoints:
// the (created_at, id) of a boundary row plus the traversal direction. Encoded
// opaque so clients cannot depend on its structure (#398, #464).
type auditCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"i"`
	Dir       string    `json:"d,omitempty"` // "" / "f" = older (forward), "b" = newer (backward)
}

const (
	// dirForward walks toward older entries (created_at DESC continues).
	dirForward = "f"
	// dirBackward walks toward newer entries.
	dirBackward = "b"
)

func encodeAuditCursor(createdAt time.Time, id, dir string) string {
	if dir == "" {
		dir = dirForward
	}
	b, _ := json.Marshal(auditCursor{CreatedAt: createdAt.UTC(), ID: id, Dir: dir})
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
	switch c.Dir {
	case "", dirForward:
		c.Dir = dirForward
	case dirBackward:
		// ok
	default:
		return nil, fmt.Errorf("invalid cursor direction")
	}
	return &c, nil
}

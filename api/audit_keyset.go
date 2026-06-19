package api

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// errAuditAnchorNotFound is returned by fetchAroundPage when the anchor entry
// id does not exist; handlers map it to 404 (#464).
var errAuditAnchorNotFound = errors.New("audit anchor entry not found")

// fetchKeysetPage runs a bidirectional keyset query and computes prev/next
// cursors. newQuery returns a fresh FILTERED query (Model set, no
// order/limit/cursor) — it is called multiple times (page query + two EXISTS
// probes). Returned rows are always in display order: created_at DESC, id DESC.
// keyOf extracts (created_at, id) from a row. The expanded comparison form and
// explicit ASC/DESC are Oracle-safe. A composite (created_at, id) index on each
// audit table (idx_sysaudit_created_id, idx_audit_created_id) serves both scan
// directions and the unfiltered full-table export without a separate sort step
// (#473). (#464)
// SEM@11dcb0b70b9f3c44dea71d422da11ebe39a116f6: fetch an audit page with bidirectional keyset cursor and compute prev/next cursors (reads DB)
func fetchKeysetPage[T any](
	newQuery func() *gorm.DB,
	cursor *auditCursor,
	limit int,
	keyOf func(T) (time.Time, string),
) ([]T, *string, *string, error) {
	backward := cursor != nil && cursor.Dir == dirBackward

	q := newQuery()
	switch {
	case cursor != nil && backward:
		q = q.Where("created_at > ? OR (created_at = ? AND id > ?)",
			cursor.CreatedAt, cursor.CreatedAt, cursor.ID).
			Order("created_at ASC, id ASC")
	case cursor != nil:
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)",
			cursor.CreatedAt, cursor.CreatedAt, cursor.ID).
			Order("created_at DESC, id DESC")
	default:
		q = q.Order("created_at DESC, id DESC")
	}

	var rows []T
	if err := q.Limit(limit).Find(&rows).Error; err != nil {
		return nil, nil, nil, err
	}
	if backward {
		reverse(rows)
	}
	if len(rows) == 0 {
		return rows, nil, nil, nil
	}

	firstT, firstID := keyOf(rows[0])
	lastT, lastID := keyOf(rows[len(rows)-1])
	prev, err := keysetCursorIfExists(newQuery(), firstT, firstID, dirBackward)
	if err != nil {
		return nil, nil, nil, err
	}
	next, err := keysetCursorIfExists(newQuery(), lastT, lastID, dirForward)
	if err != nil {
		return nil, nil, nil, err
	}
	return rows, prev, next, nil
}

// fetchAroundPage returns a page of `limit` rows centered on the anchor entry,
// with ~half newer and ~half older. fetchAnchor loads the anchor by id ignoring
// filters; a nil anchor yields errAuditAnchorNotFound. Surrounding rows respect
// the filters baked into newQuery. The anchor is always included and centered.
// SEM@11dcb0b70b9f3c44dea71d422da11ebe39a116f6: fetch an audit page centered on an anchor entry with surrounding rows and cursors (reads DB)
func fetchAroundPage[T any](
	newQuery func() *gorm.DB,
	fetchAnchor func() (*T, error),
	limit int,
	keyOf func(T) (time.Time, string),
) ([]T, *string, *string, error) {
	anchor, err := fetchAnchor()
	if err != nil {
		return nil, nil, nil, err
	}
	if anchor == nil {
		return nil, nil, nil, errAuditAnchorNotFound
	}
	anchorT, anchorID := keyOf(*anchor)

	newerWant := (limit - 1) / 2
	newer, err := fetchSide[T](newQuery(), anchorT, anchorID, dirBackward, newerWant)
	if err != nil {
		return nil, nil, nil, err
	}
	olderWant := limit - 1 - len(newer)
	older, err := fetchSide[T](newQuery(), anchorT, anchorID, dirForward, olderWant)
	if err != nil {
		return nil, nil, nil, err
	}
	// Backfill the newer side when the older side was deficient, so the page
	// fills to `limit` whenever enough rows exist on either side.
	if len(newer)+len(older)+1 < limit {
		newerWant2 := limit - 1 - len(older)
		if newerWant2 > len(newer) {
			newer, err = fetchSide[T](newQuery(), anchorT, anchorID, dirBackward, newerWant2)
			if err != nil {
				return nil, nil, nil, err
			}
		}
	}

	reverse(newer) // ASC closest-first -> display order newest->oldest
	page := make([]T, 0, len(newer)+1+len(older))
	page = append(page, newer...)
	page = append(page, *anchor)
	page = append(page, older...) // DESC closest-first == newest->oldest

	firstT, firstID := keyOf(page[0])
	lastT, lastID := keyOf(page[len(page)-1])
	prev, err := keysetCursorIfExists(newQuery(), firstT, firstID, dirBackward)
	if err != nil {
		return nil, nil, nil, err
	}
	next, err := keysetCursorIfExists(newQuery(), lastT, lastID, dirForward)
	if err != nil {
		return nil, nil, nil, err
	}
	return page, prev, next, nil
}

// fetchSide returns up to n rows on one side of the anchor, ordered
// closest-to-anchor first. dirBackward = newer rows; dirForward = older rows.
// SEM@11dcb0b70b9f3c44dea71d422da11ebe39a116f6: fetch up to n audit rows on one side of an anchor ordered closest-first (reads DB)
func fetchSide[T any](q *gorm.DB, t time.Time, id, dir string, n int) ([]T, error) {
	if n <= 0 {
		return nil, nil
	}
	if dir == dirBackward {
		q = q.Where("created_at > ? OR (created_at = ? AND id > ?)", t, t, id).
			Order("created_at ASC, id ASC")
	} else {
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", t, t, id).
			Order("created_at DESC, id DESC")
	}
	var rows []T
	if err := q.Limit(n).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// keysetCursorIfExists returns an encoded cursor anchored at (t, id) in the
// given direction, or nil when no row exists beyond that boundary. Uses an
// indexed SELECT id ... LIMIT 1 probe.
// SEM@11dcb0b70b9f3c44dea71d422da11ebe39a116f6: return an encoded keyset cursor if rows exist beyond the given boundary, else nil (reads DB)
func keysetCursorIfExists(q *gorm.DB, t time.Time, id, dir string) (*string, error) {
	var cmp string
	if dir == dirBackward {
		cmp = "created_at > ? OR (created_at = ? AND id > ?)"
	} else {
		cmp = "created_at < ? OR (created_at = ? AND id < ?)"
	}
	var ids []string
	if err := q.Where(cmp, t, t, id).Select("id").Limit(1).Find(&ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	enc := encodeAuditCursor(t, id, dir)
	return &enc, nil
}

// reverse reverses a slice in place.
// SEM@11dcb0b70b9f3c44dea71d422da11ebe39a116f6: reverse a slice in place (pure)
func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

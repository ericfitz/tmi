//go:build dev || test || integration

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// newSysAuditEntry constructs a minimal SystemAuditEntry ready for insertion.
// actorEmail is the per-test unique value used for isolation.
func newSysAuditEntry(actorEmail string) models.SystemAuditEntry {
	return models.SystemAuditEntry{
		ID:               models.DBVarchar(uuid.New().String()),
		ActorEmail:       models.DBVarchar(actorEmail),
		ActorProvider:    models.DBVarchar("tmi"),
		ActorProviderID:  models.DBVarchar(actorEmail),
		ActorDisplayName: models.DBVarchar("Integration Test User"),
		HTTPMethod:       models.DBVarchar("PUT"),
		HTTPPath:         models.DBText("/admin/test"),
		FieldPath:        models.DBVarchar("test.field"),
	}
}

// insertSysAuditWithTime inserts a SystemAuditEntry with the given created_at
// timestamp. GORM's autoCreateTime respects a non-zero CreatedAt set on the
// struct, so we set it directly rather than issuing a backdating UPDATE. This
// avoids hitting the append-only trigger that is installed on the production
// and dev databases (the trigger blocks UPDATE on system_audit_entries).
func insertSysAuditWithTime(t *testing.T, db *gorm.DB, actorEmail string, ts time.Time) string {
	t.Helper()
	e := newSysAuditEntry(actorEmail)
	e.CreatedAt = ts
	id := string(e.ID)
	require.NoError(t, db.Create(&e).Error)
	return id
}

// TestBidirectionalTraversalIntegration seeds N rows with strictly distinct
// created_at values, pages forward collecting all ids, then verifies backward
// traversal from a mid-cursor returns the correct newer page — with no
// duplicates or gaps. Validates the keyset SQL on a real PostgreSQL database.
func TestBidirectionalTraversalIntegration(t *testing.T) {
	// openAppendOnlyIntegrationDB skips the test when TEST_DB_* are unset.
	db := openAppendOnlyIntegrationDB(t)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))

	ctx := context.Background()
	actor := "pg-bidir-" + uuid.New().String() + "@tmi.local"
	repo := NewSystemAuditRepository(db)

	// Seed 7 rows with strictly increasing created_at (1s apart) so that
	// keyset ordering is deterministic on PostgreSQL (microsecond precision).
	const n = 7
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Duration(n) * time.Second)
	var seedIDs []string
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		id := insertSysAuditWithTime(t, db, actor, ts)
		seedIDs = append(seedIDs, id)
	}
	// seedIDs[0] = oldest, seedIDs[n-1] = newest (by created_at).
	// List returns newest first so display order is reversed.

	f := SystemAuditFilter{ActorEmail: &actor, Limit: 2}

	// --- Forward traversal: collect all pages ---
	var forwardIDs []string
	var lastNext *string
	var pageCursors []*string // next cursor of each full page (for backward probe)
	curFilter := f
	for {
		rows, total, _, next, err := repo.List(ctx, curFilter)
		require.NoError(t, err)
		assert.Equal(t, n, total, "total must always equal n")
		for _, r := range rows {
			forwardIDs = append(forwardIDs, string(r.ID))
		}
		lastNext = next
		if len(rows) > 0 && next != nil {
			pageCursors = append(pageCursors, next)
		}
		if next == nil {
			break
		}
		decoded, err := decodeAuditCursor(*next)
		require.NoError(t, err)
		curFilter = SystemAuditFilter{ActorEmail: &actor, Limit: 2, Cursor: decoded}
	}

	// No duplicates in forward traversal.
	seen := map[string]bool{}
	for _, id := range forwardIDs {
		assert.False(t, seen[id], "duplicate id in forward traversal: %s", id)
		seen[id] = true
	}
	// No gaps: all seeded ids appear.
	assert.Len(t, seen, n, "forward traversal must visit all rows")

	// The last page must NOT have a next cursor (oldest boundary reached).
	assert.Nil(t, lastNext, "last page next must be nil")

	// --- Backward traversal: page 2's prev cursor must reproduce page 1 ---
	if len(pageCursors) < 1 {
		t.Skip("not enough pages to test backward traversal")
	}
	page1Filter := SystemAuditFilter{ActorEmail: &actor, Limit: 2}
	page1Rows, _, _, page1Next, err := repo.List(ctx, page1Filter)
	require.NoError(t, err)
	require.NotNil(t, page1Next, "page 1 must yield a next cursor")

	decoded2, err := decodeAuditCursor(*page1Next)
	require.NoError(t, err)
	page2Filter := SystemAuditFilter{ActorEmail: &actor, Limit: 2, Cursor: decoded2}
	page2Rows, _, page2Prev, _, err := repo.List(ctx, page2Filter)
	require.NoError(t, err)
	require.NotNil(t, page2Prev, "page 2 must yield a prev cursor")

	// Walk backward from page2's prev cursor: must reproduce page 1.
	decodedPrev, err := decodeAuditCursor(*page2Prev)
	require.NoError(t, err)
	backFilter := SystemAuditFilter{ActorEmail: &actor, Limit: 2, Cursor: decodedPrev}
	backRows, _, _, _, err := repo.List(ctx, backFilter)
	require.NoError(t, err)
	require.Len(t, backRows, len(page1Rows), "backward page must have same length as page 1")
	for i := range page1Rows {
		assert.Equal(t, string(page1Rows[i].ID), string(backRows[i].ID),
			"backward traversal must reproduce page 1 at index %d", i)
	}

	// Page 1 and page 2 must not overlap.
	p1Set := map[string]bool{}
	for _, r := range page1Rows {
		p1Set[string(r.ID)] = true
	}
	for _, r := range page2Rows {
		assert.False(t, p1Set[string(r.ID)], "page 2 must not overlap page 1, but found %s", string(r.ID))
	}
}

// TestKeysetStabilityUnderConcurrentInsertIntegration seeds rows, captures the
// next cursor from page 1, inserts a NEW newest row, then fetches page 2 via
// the captured cursor and asserts it does not contain the new row and does not
// skip any of the original seeds.
func TestKeysetStabilityUnderConcurrentInsertIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))

	ctx := context.Background()
	actor := "pg-stable-" + uuid.New().String() + "@tmi.local"
	repo := NewSystemAuditRepository(db)

	// Seed 4 rows with strictly distinct timestamps.
	base := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	var seedIDs []string
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		id := insertSysAuditWithTime(t, db, actor, ts)
		seedIDs = append(seedIDs, id)
	}

	// Fetch page 1 (limit=2) and capture the next cursor.
	f := SystemAuditFilter{ActorEmail: &actor, Limit: 2}
	page1, _, _, next, err := repo.List(ctx, f)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotNil(t, next, "seeding 4 rows with limit 2 must yield next cursor")

	// "Concurrent" insert: a brand-new newest row (5 seconds into the future
	// relative to wall clock — definitely newer than all seeded rows).
	newTS := time.Now().UTC().Add(5 * time.Second)
	newID := insertSysAuditWithTime(t, db, actor, newTS)

	// Fetch page 2 with the cursor captured BEFORE the insert.
	decoded, err := decodeAuditCursor(*next)
	require.NoError(t, err)
	f2 := SystemAuditFilter{ActorEmail: &actor, Limit: 2, Cursor: decoded}
	page2, _, _, _, err := repo.List(ctx, f2)
	require.NoError(t, err)

	// The new row must NOT appear in page 2 (cursor is anchored to the page-1
	// boundary, which is older than newTS).
	for _, r := range page2 {
		assert.NotEqual(t, newID, string(r.ID),
			"newly inserted newest row must not bleed into older-anchored page 2")
	}

	// Page 1 and page 2 must not overlap.
	p1Set := map[string]bool{}
	for _, r := range page1 {
		p1Set[string(r.ID)] = true
	}
	for _, r := range page2 {
		assert.False(t, p1Set[string(r.ID)], "page 2 must not overlap page 1: %s", string(r.ID))
	}

	// All original 4 seeds must appear across page1+page2.
	allIDs := map[string]bool{}
	for _, r := range append(page1, page2...) {
		allIDs[string(r.ID)] = true
	}
	for _, id := range seedIDs {
		assert.True(t, allIDs[id], "seeded id %s must appear in page1 or page2", id)
	}
}

// TestAroundIntegration seeds 7 rows, calls Around on the middle entry, and
// asserts the page is centered with non-nil cursors on both sides. Also
// verifies a random uuid returns errAuditAnchorNotFound.
func TestAroundIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))

	ctx := context.Background()
	actor := "pg-around-" + uuid.New().String() + "@tmi.local"
	repo := NewSystemAuditRepository(db)

	// Seed 7 rows strictly ordered by created_at.
	// ids[0] = oldest, ids[6] = newest.
	const n = 7
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Duration(n) * time.Second)
	var ids []string
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		id := insertSysAuditWithTime(t, db, actor, ts)
		ids = append(ids, id)
	}
	// Display order (newest-first): ids[6], ids[5], ids[4], ids[3], ids[2], ids[1], ids[0].
	// Choose ids[3] as the anchor (display index 3 = middle).
	// Around with limit=5 fetches newerWant=2 (ids[4],ids[5]) + olderWant=2 (ids[2],ids[1]).
	// Page: [ids[5], ids[4], ids[3], ids[2], ids[1]] → anchor at display index 2.
	// prev is non-nil because ids[6] exists beyond the newest page row (ids[5]).
	// next is non-nil because ids[0] exists beyond the oldest page row (ids[1]).
	midID := ids[3]

	f := SystemAuditFilter{ActorEmail: &actor, Limit: 5}
	rows, total, prev, next, err := repo.Around(ctx, f, midID)
	require.NoError(t, err)
	assert.Equal(t, n, total)
	require.Len(t, rows, 5)

	// Anchor must be at index 2 (0-based center of a 5-element page).
	assert.Equal(t, midID, string(rows[2].ID),
		"anchor must be at index 2 (center of the 5-element page)")

	// Both cursors must be non-nil:
	// prev: ids[6] is newer than the first page row (ids[5]).
	// next: ids[0] is older than the last page row (ids[1]).
	assert.NotNil(t, prev, "prev cursor must not be nil: ids[6] exists beyond the page")
	assert.NotNil(t, next, "next cursor must not be nil: ids[0] exists beyond the page")

	// Unknown anchor must return errAuditAnchorNotFound.
	randomID := uuid.New().String()
	_, _, _, _, err = repo.Around(ctx, f, randomID)
	require.ErrorIs(t, err, errAuditAnchorNotFound,
		"unknown anchor must return errAuditAnchorNotFound")
}

// TestStreamFilteredIntegration seeds rows for two actors, streams with actorA
// filter using a small batch size, and asserts only actorA rows are streamed
// and they arrive in multiple batches.
func TestStreamFilteredIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))

	ctx := context.Background()
	actorA := "pg-stream-a-" + uuid.New().String() + "@tmi.local"
	actorB := "pg-stream-b-" + uuid.New().String() + "@tmi.local"
	repo := NewSystemAuditRepository(db)

	// Seed 5 rows for actorA and 3 for actorB with non-overlapping timestamps.
	base := time.Now().UTC().Truncate(time.Second).Add(-20 * time.Second)
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		insertSysAuditWithTime(t, db, actorA, ts)
	}
	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i+10) * time.Second)
		insertSysAuditWithTime(t, db, actorB, ts)
	}

	// Stream with batch=2 to exercise the multi-batch code path.
	f := SystemAuditFilter{ActorEmail: &actorA}
	var totalStreamed int
	var batchCount int
	err := repo.StreamFiltered(ctx, f, 2, func(rows []models.SystemAuditEntry) error {
		batchCount++
		for _, r := range rows {
			assert.Equal(t, actorA, string(r.ActorEmail),
				"streamed row must belong to actorA only")
			totalStreamed++
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 5, totalStreamed, "must stream exactly 5 rows for actorA")
	assert.GreaterOrEqual(t, batchCount, 2,
		"5 rows with batch=2 must yield at least 2 callbacks")
}

// TestExportHandlerCSVAndNDJSONIntegration exercises the ListSystemAuditEntries
// handler with format=csv and format=ndjson against the real PG-backed
// repository, verifying headers, Content-Disposition, and row counts.
func TestExportHandlerCSVAndNDJSONIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))

	ctx := context.Background()
	actor := "pg-export-" + uuid.New().String() + "@tmi.local"
	repo := NewSystemAuditRepository(db)

	// Seed 3 rows.
	base := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		insertSysAuditWithTime(t, db, actor, ts)
	}

	gin.SetMode(gin.TestMode)
	s := &Server{}
	s.SetSystemAuditRepo(repo)

	actorEmail := AuditActorEmail(openapi_types.Email(actor))

	t.Run("csv", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/admin/audit/system?format=csv&actor_email=%s", actor), nil)
		c.Request = c.Request.WithContext(ctx)

		fmtCSV := ListSystemAuditEntriesParamsFormatCsv
		s.ListSystemAuditEntries(c, ListSystemAuditEntriesParams{
			Format:     &fmtCSV,
			ActorEmail: &actorEmail,
		})

		assert.Equal(t, http.StatusOK, w.Code, "CSV export must return 200")
		cd := w.Header().Get("Content-Disposition")
		assert.True(t, strings.HasPrefix(cd, `attachment; filename="system-audit-`),
			"Content-Disposition must start with attachment; filename=\"system-audit-, got: %q", cd)

		body := w.Body.String()
		lines := nonEmptyLinesAudit(body)
		require.GreaterOrEqual(t, len(lines), 1, "must have at least a header line")
		assert.Equal(t, strings.Join(systemAuditCSVHeader, ","), lines[0],
			"first CSV line must be the column header")
		dataLines := lines[1:]
		assert.Len(t, dataLines, 3, "must have exactly 3 data rows")
	})

	t.Run("ndjson", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/admin/audit/system?format=ndjson&actor_email=%s", actor), nil)
		c.Request = c.Request.WithContext(ctx)

		fmtNDJSON := ListSystemAuditEntriesParamsFormatNdjson
		s.ListSystemAuditEntries(c, ListSystemAuditEntriesParams{
			Format:     &fmtNDJSON,
			ActorEmail: &actorEmail,
		})

		assert.Equal(t, http.StatusOK, w.Code, "NDJSON export must return 200")
		ct := w.Header().Get("Content-Type")
		assert.Equal(t, "application/x-ndjson", ct,
			"Content-Type must be application/x-ndjson")

		// Each non-empty line must be valid JSON.
		var lineCount int
		scanner := bufio.NewScanner(bytes.NewReader(w.Body.Bytes()))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var m map[string]interface{}
			err := json.Unmarshal([]byte(line), &m)
			assert.NoError(t, err, "each NDJSON line must be valid JSON: %q", line)
			lineCount++
		}
		assert.Equal(t, 3, lineCount, "must have exactly 3 NDJSON lines")
	})
}

// TestDefaultJSONCursorsIntegration calls ListSystemAuditEntries with no
// format/around and a small limit, and asserts the response JSON contains both
// next_cursor and prev_cursor keys (even when they are null).
func TestDefaultJSONCursorsIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))

	ctx := context.Background()
	actor := "pg-json-cursors-" + uuid.New().String() + "@tmi.local"
	repo := NewSystemAuditRepository(db)

	// Seed 5 rows so that with limit=2 we get a next cursor.
	base := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	for i := 0; i < 5; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		insertSysAuditWithTime(t, db, actor, ts)
	}

	gin.SetMode(gin.TestMode)
	s := &Server{}
	s.SetSystemAuditRepo(repo)

	actorEmail := AuditActorEmail(openapi_types.Email(actor))
	limit := AuditPageLimit(2)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/audit/system?limit=2", nil)
	c.Request = c.Request.WithContext(ctx)

	s.ListSystemAuditEntries(c, ListSystemAuditEntriesParams{
		ActorEmail: &actorEmail,
		Limit:      &limit,
	})

	assert.Equal(t, http.StatusOK, w.Code, "default JSON must return 200")

	var body map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err, "response must be valid JSON")

	_, hasNext := body["next_cursor"]
	_, hasPrev := body["prev_cursor"]
	assert.True(t, hasNext, "JSON response must contain next_cursor key")
	assert.True(t, hasPrev, "JSON response must contain prev_cursor key")

	// With 5 rows and limit=2, next_cursor must be a non-nil string.
	assert.NotNil(t, body["next_cursor"], "next_cursor must be non-nil with 5 rows and limit=2")
}

// nonEmptyLinesAudit splits s by newlines, strips trailing \r, and returns
// non-empty lines. Used by CSV export assertions.
func nonEmptyLinesAudit(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimRight(line, "\r"); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

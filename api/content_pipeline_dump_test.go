package api

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNoteRepoForDump captures Create calls and otherwise stubs the interface.
type fakeNoteRepoForDump struct {
	mu       sync.Mutex
	created  []capturedNote
	createFn func(ctx context.Context, note *Note, threatModelID string) error
}

type capturedNote struct {
	threatModelID string
	name          string
	content       string
}

func (f *fakeNoteRepoForDump) Create(ctx context.Context, note *Note, threatModelID string) error {
	if f.createFn != nil {
		if err := f.createFn(ctx, note, threatModelID); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, capturedNote{
		threatModelID: threatModelID,
		name:          note.Name,
		content:       note.Content,
	})
	return nil
}

func (f *fakeNoteRepoForDump) Get(_ context.Context, _ string) (*Note, error) { return nil, nil }
func (f *fakeNoteRepoForDump) Update(_ context.Context, _ *Note, _ string) error {
	return nil
}
func (f *fakeNoteRepoForDump) Delete(_ context.Context, _ string) error     { return nil }
func (f *fakeNoteRepoForDump) SoftDelete(_ context.Context, _ string) error { return nil }
func (f *fakeNoteRepoForDump) Restore(_ context.Context, _ string) error    { return nil }
func (f *fakeNoteRepoForDump) HardDelete(_ context.Context, _ string) error { return nil }
func (f *fakeNoteRepoForDump) GetIncludingDeleted(_ context.Context, _ string) (*Note, error) {
	return nil, nil
}
func (f *fakeNoteRepoForDump) Patch(_ context.Context, _ string, _ []PatchOperation) (*Note, error) {
	return nil, nil
}
func (f *fakeNoteRepoForDump) List(_ context.Context, _ string, _, _ int) ([]Note, error) {
	return nil, nil
}
func (f *fakeNoteRepoForDump) Count(_ context.Context, _ string) (int, error) { return 0, nil }
func (f *fakeNoteRepoForDump) InvalidateCache(_ context.Context, _ string) error {
	return nil
}
func (f *fakeNoteRepoForDump) WarmCache(_ context.Context, _ string) error { return nil }

// fakeDocRepoForDump implements DocumentRepository just enough for the dump hook.
type fakeDocRepoForDump struct {
	threatModelID string
	getTMErr      error
}

func (f *fakeDocRepoForDump) GetThreatModelID(_ context.Context, _ string) (string, error) {
	return f.threatModelID, f.getTMErr
}
func (f *fakeDocRepoForDump) Create(_ context.Context, _ *Document, _ string) error { return nil }
func (f *fakeDocRepoForDump) Get(_ context.Context, _ string) (*Document, error)    { return nil, nil }
func (f *fakeDocRepoForDump) Update(_ context.Context, _ *Document, _ string) error { return nil }
func (f *fakeDocRepoForDump) Delete(_ context.Context, _ string) error              { return nil }
func (f *fakeDocRepoForDump) SoftDelete(_ context.Context, _ string) error          { return nil }
func (f *fakeDocRepoForDump) Restore(_ context.Context, _ string) error             { return nil }
func (f *fakeDocRepoForDump) HardDelete(_ context.Context, _ string) error          { return nil }
func (f *fakeDocRepoForDump) GetIncludingDeleted(_ context.Context, _ string) (*Document, error) {
	return nil, nil
}
func (f *fakeDocRepoForDump) Patch(_ context.Context, _ string, _ []PatchOperation) (*Document, error) {
	return nil, nil
}
func (f *fakeDocRepoForDump) List(_ context.Context, _ string, _, _ int) ([]Document, error) {
	return nil, nil
}
func (f *fakeDocRepoForDump) ListByAccessStatus(_ context.Context, _ string, _ int) ([]Document, error) {
	return nil, nil
}
func (f *fakeDocRepoForDump) Count(_ context.Context, _ string) (int, error) { return 0, nil }
func (f *fakeDocRepoForDump) BulkCreate(_ context.Context, _ []Document, _ string) error {
	return nil
}
func (f *fakeDocRepoForDump) UpdateAccessStatus(_ context.Context, _, _, _ string) error {
	return nil
}
func (f *fakeDocRepoForDump) UpdateAccessStatusWithDiagnostics(_ context.Context, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeDocRepoForDump) GetAccessReason(_ context.Context, _ string) (string, string, *time.Time, error) {
	return "", "", nil, nil
}
func (f *fakeDocRepoForDump) GetPickerDispatch(_ context.Context, _ string) (*PickerMetadata, string, error) {
	return nil, "", nil
}
func (f *fakeDocRepoForDump) SetPickerMetadata(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (f *fakeDocRepoForDump) ClearPickerMetadataForOwner(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}
func (f *fakeDocRepoForDump) InvalidateCache(_ context.Context, _ string) error { return nil }
func (f *fakeDocRepoForDump) WarmCache(_ context.Context, _ string) error       { return nil }

func newPipelineWithExtractor(extracted string) *ContentPipeline {
	sources := NewContentSourceRegistry()
	sources.Register(&mockSource{
		name:      "test-src",
		canHandle: true,
		data:      []byte("raw"),
		ct:        "text/markdown",
	})
	extractors := NewContentExtractorRegistry()
	extractors.Register(&mockExtractor{
		name:      "md",
		canHandle: true,
		result:    ExtractedContent{Text: extracted, ContentType: "text/markdown"},
	})
	return NewContentPipeline(sources, extractors, NewURLPatternMatcher())
}

func newPipelineWithFailingExtractor() *ContentPipeline {
	sources := NewContentSourceRegistry()
	sources.Register(&mockSource{
		name:      "test-src",
		canHandle: true,
		data:      []byte("raw"),
		ct:        "text/markdown",
	})
	extractors := NewContentExtractorRegistry()
	extractors.Register(&mockExtractor{
		name:      "md",
		canHandle: true,
		err:       errors.New("boom"),
	})
	return NewContentPipeline(sources, extractors, NewURLPatternMatcher())
}

func docForDump() Document {
	id := uuid.New()
	return Document{
		Id:   &id,
		Name: "design.docx",
		Uri:  "https://example.com/design.docx",
	}
}

// TestExtractForDocument_NoDumper_NoNoteCreated verifies the dev-only hook is
// strictly opt-in: with no dumper configured, ExtractForDocument behaves
// identically to Extract and writes no Note.
func TestExtractForDocument_NoDumper_NoNoteCreated(t *testing.T) {
	pipeline := newPipelineWithExtractor("# extracted markdown\nbody")
	notes := &fakeNoteRepoForDump{}
	// Note: dumper not set on pipeline.

	out, err := pipeline.ExtractForDocument(context.Background(), docForDump())
	require.NoError(t, err)
	assert.Equal(t, "# extracted markdown\nbody", out.Text)
	assert.Empty(t, notes.created, "no Note should be created when dumper is nil")
}

// TestExtractForDocument_DumperOn_NoteCreated verifies the happy path: with
// the dumper configured, a successful extraction persists a Note attributed
// to the document's parent threat model with the extracted markdown as body.
func TestExtractForDocument_DumperOn_NoteCreated(t *testing.T) {
	pipeline := newPipelineWithExtractor("# extracted markdown\nbody")
	tmID := uuid.New().String()
	notes := &fakeNoteRepoForDump{}
	docs := &fakeDocRepoForDump{threatModelID: tmID}
	pipeline.SetExtractedTextNoteDumper(NewExtractedTextNoteDumper(notes, docs))

	doc := docForDump()
	out, err := pipeline.ExtractForDocument(context.Background(), doc)
	require.NoError(t, err)
	assert.Equal(t, "# extracted markdown\nbody", out.Text)

	require.Len(t, notes.created, 1)
	assert.Equal(t, tmID, notes.created[0].threatModelID)
	assert.Equal(t, "# extracted markdown\nbody", notes.created[0].content)
	assert.True(t, strings.HasPrefix(notes.created[0].name, "[extracted] design.docx @ "),
		"got name %q", notes.created[0].name)
}

// TestExtractForDocument_DumperOn_ExtractionFails_NoNote verifies that when
// extraction itself fails, no Note is dumped — failures take the existing
// classify-and-persist-diagnostic path.
func TestExtractForDocument_DumperOn_ExtractionFails_NoNote(t *testing.T) {
	pipeline := newPipelineWithFailingExtractor()
	notes := &fakeNoteRepoForDump{}
	docs := &fakeDocRepoForDump{threatModelID: uuid.New().String()}
	pipeline.SetExtractedTextNoteDumper(NewExtractedTextNoteDumper(notes, docs))

	_, err := pipeline.ExtractForDocument(context.Background(), docForDump())
	require.Error(t, err)
	assert.Empty(t, notes.created, "no Note should be created when extraction fails")
}

// TestExtractForDocument_DumperOn_NoThreatModel_NoNote verifies the defensive
// skip when the document has no parent threat model (shouldn't normally
// happen, but we want a clean no-op rather than a write to a bogus tm_id).
func TestExtractForDocument_DumperOn_NoThreatModel_NoNote(t *testing.T) {
	pipeline := newPipelineWithExtractor("body")
	notes := &fakeNoteRepoForDump{}
	docs := &fakeDocRepoForDump{threatModelID: ""}
	pipeline.SetExtractedTextNoteDumper(NewExtractedTextNoteDumper(notes, docs))

	_, err := pipeline.ExtractForDocument(context.Background(), docForDump())
	require.NoError(t, err)
	assert.Empty(t, notes.created)
}

// TestExtractForDocument_DumperOn_NoteWriteFails_DoesNotAffectExtract verifies
// that a failure to persist the dump-Note is logged but does not propagate to
// the caller — the inspection aid must not change pipeline behavior.
func TestExtractForDocument_DumperOn_NoteWriteFails_DoesNotAffectExtract(t *testing.T) {
	pipeline := newPipelineWithExtractor("body")
	notes := &fakeNoteRepoForDump{
		createFn: func(_ context.Context, _ *Note, _ string) error {
			return errors.New("db unavailable")
		},
	}
	docs := &fakeDocRepoForDump{threatModelID: uuid.New().String()}
	pipeline.SetExtractedTextNoteDumper(NewExtractedTextNoteDumper(notes, docs))

	out, err := pipeline.ExtractForDocument(context.Background(), docForDump())
	require.NoError(t, err)
	assert.Equal(t, "body", out.Text)
}

// TestPipelineEmbeddingSource_DocumentEntity_FiresDump verifies that the
// session-indexing path (Timmy snapshotting documents into the vector index)
// also triggers the dev-mode dump when the dumper is configured. Acceptance
// criterion: "every successful extraction also persists a Note".
func TestPipelineEmbeddingSource_DocumentEntity_FiresDump(t *testing.T) {
	pipeline := newPipelineWithExtractor("# from session indexing")
	tmID := uuid.New().String()
	notes := &fakeNoteRepoForDump{}
	docs := &fakeDocRepoForDump{threatModelID: tmID}
	pipeline.SetExtractedTextNoteDumper(NewExtractedTextNoteDumper(notes, docs))

	src := NewPipelineEmbeddingSource(pipeline)
	docID := uuid.New().String()
	out, err := src.Extract(context.Background(), EntityReference{
		EntityType: "document",
		EntityID:   docID,
		URI:        "https://example.com/spec.docx",
		Name:       "spec.docx",
	})
	require.NoError(t, err)
	assert.Equal(t, "# from session indexing", out.Text)

	require.Len(t, notes.created, 1, "session-indexing path of a document must dump when dumper is wired")
	assert.Equal(t, tmID, notes.created[0].threatModelID)
	assert.Equal(t, "# from session indexing", notes.created[0].content)
	assert.True(t, strings.HasPrefix(notes.created[0].name, "[extracted] spec.docx @ "),
		"got name %q", notes.created[0].name)
}

// TestPipelineEmbeddingSource_NonDocumentEntity_DoesNotDump verifies that
// non-document URI-bearing entities go through the plain Extract path and
// do NOT fire the dump (the dump is keyed on documents — there's no parent
// threat-model lookup for arbitrary URLs).
func TestPipelineEmbeddingSource_NonDocumentEntity_DoesNotDump(t *testing.T) {
	pipeline := newPipelineWithExtractor("body")
	notes := &fakeNoteRepoForDump{}
	docs := &fakeDocRepoForDump{threatModelID: uuid.New().String()}
	pipeline.SetExtractedTextNoteDumper(NewExtractedTextNoteDumper(notes, docs))

	src := NewPipelineEmbeddingSource(pipeline)
	_, err := src.Extract(context.Background(), EntityReference{
		EntityType: "repository",
		EntityID:   uuid.New().String(),
		URI:        "https://github.com/owner/repo",
		Name:       "owner/repo",
	})
	require.NoError(t, err)
	assert.Empty(t, notes.created, "non-document entity must not dump")
}

// TestPipelineEmbeddingSource_DocumentEntity_BadID_FallsBackToExtract verifies
// the defensive fallback: a malformed document EntityID falls through to the
// plain Extract path rather than panicking. The dump simply doesn't fire for
// that call.
func TestPipelineEmbeddingSource_DocumentEntity_BadID_FallsBackToExtract(t *testing.T) {
	pipeline := newPipelineWithExtractor("body")
	notes := &fakeNoteRepoForDump{}
	docs := &fakeDocRepoForDump{threatModelID: uuid.New().String()}
	pipeline.SetExtractedTextNoteDumper(NewExtractedTextNoteDumper(notes, docs))

	src := NewPipelineEmbeddingSource(pipeline)
	out, err := src.Extract(context.Background(), EntityReference{
		EntityType: "document",
		EntityID:   "not-a-uuid",
		URI:        "https://example.com/x.docx",
		Name:       "x.docx",
	})
	require.NoError(t, err)
	assert.Equal(t, "body", out.Text)
	assert.Empty(t, notes.created, "malformed document ID must not dump")
}

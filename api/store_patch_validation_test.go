package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover validation that used to live in the GORM BeforeSave hooks
// (Document.BeforeSave, Repository.BeforeSave) and was silently lost when the
// Update paths moved to Session{SkipHooks: true} to stop a map-based update
// validating an empty model struct (#610).
//
// The gap mattered because ValidateURIPatchOperations deliberately skips empty
// strings, so `replace /uri ""` reached the store unchecked. PostgreSQL stores
// '' in a NOT NULL text/CLOB column; Oracle binds '' as NULL and raises
// ORA-01407. That is a dialect divergence AND a 500 on the production dialect
// only, so the check has to happen before the write on both.

func TestRepositoryPatch_RejectsEmptyURI(t *testing.T) {
	s := &GormRepositoryRepository{}
	repo := &Repository{Uri: "https://example.com/repo.git"}

	err := s.applyPatchOperation(repo, PatchOperation{
		Op:    string(Replace),
		Path:  PatchPathURI,
		Value: "",
	})

	require.Error(t, err, "an empty URI must be rejected before it reaches the DB")
	assert.Contains(t, err.Error(), "uri")
	assert.Equal(t, "https://example.com/repo.git", repo.Uri,
		"the rejected value must not be applied to the struct")
}

func TestRepositoryPatch_RejectsWhitespaceOnlyURI(t *testing.T) {
	s := &GormRepositoryRepository{}
	repo := &Repository{Uri: "https://example.com/repo.git"}

	err := s.applyPatchOperation(repo, PatchOperation{
		Op:    string(Replace),
		Path:  PatchPathURI,
		Value: "   ",
	})

	require.Error(t, err, "a whitespace-only URI trims to empty and must be rejected")
}

func TestRepositoryPatch_AcceptsValidURI(t *testing.T) {
	s := &GormRepositoryRepository{}
	repo := &Repository{Uri: "https://example.com/old.git"}

	err := s.applyPatchOperation(repo, PatchOperation{
		Op:    string(Replace),
		Path:  PatchPathURI,
		Value: "https://example.com/new.git",
	})

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/new.git", repo.Uri)
}

func TestRepositoryPatch_RejectsOffEnumType(t *testing.T) {
	s := &GormRepositoryRepository{}
	repo := &Repository{Uri: "https://example.com/repo.git"}

	err := s.applyPatchOperation(repo, PatchOperation{
		Op:    string(Replace),
		Path:  PatchPathType,
		Value: "bogus",
	})

	require.Error(t, err, "an off-enum repository type must be rejected")
	assert.Nil(t, repo.Type, "the rejected value must not be applied to the struct")
}

func TestRepositoryPatch_AcceptsValidType(t *testing.T) {
	s := &GormRepositoryRepository{}
	repo := &Repository{Uri: "https://example.com/repo.git"}

	for _, valid := range []string{"git", "svn", "mercurial", "other"} {
		err := s.applyPatchOperation(repo, PatchOperation{
			Op:    string(Replace),
			Path:  PatchPathType,
			Value: valid,
		})
		require.NoError(t, err, "%q is a valid repository type", valid)
		require.NotNil(t, repo.Type)
		assert.Equal(t, valid, string(*repo.Type))
	}
}

func TestDocumentPatch_RejectsEmptyURI(t *testing.T) {
	s := &GormDocumentRepository{}
	doc := &Document{Name: "spec", Uri: "https://example.com/spec.pdf"}

	err := s.applyPatchOperation(doc, PatchOperation{
		Op:    string(Replace),
		Path:  PatchPathURI,
		Value: "",
	})

	require.Error(t, err, "an empty URI must be rejected before it reaches the DB")
	assert.Equal(t, "https://example.com/spec.pdf", doc.Uri,
		"the rejected value must not be applied to the struct")
}

func TestDocumentPatch_RejectsEmptyName(t *testing.T) {
	s := &GormDocumentRepository{}
	doc := &Document{Name: "spec", Uri: "https://example.com/spec.pdf"}

	err := s.applyPatchOperation(doc, PatchOperation{
		Op:    string(Replace),
		Path:  PatchPathName,
		Value: "",
	})

	require.Error(t, err, "NAME is VARCHAR2(256) NOT NULL on Oracle; '' binds as NULL")
	assert.Equal(t, "spec", doc.Name,
		"the rejected value must not be applied to the struct")
}

func TestDocumentPatch_AcceptsValidNameAndURI(t *testing.T) {
	s := &GormDocumentRepository{}
	doc := &Document{Name: "old", Uri: "https://example.com/old.pdf"}

	require.NoError(t, s.applyPatchOperation(doc, PatchOperation{
		Op: string(Replace), Path: PatchPathName, Value: "new",
	}))
	require.NoError(t, s.applyPatchOperation(doc, PatchOperation{
		Op: string(Replace), Path: PatchPathURI, Value: "https://example.com/new.pdf",
	}))

	assert.Equal(t, "new", doc.Name)
	assert.Equal(t, "https://example.com/new.pdf", doc.Uri)
}

// --- #614: the same hole in the sibling sub-resources ---------------------
//
// Repositories and documents were fixed by the ORA-01407 work; assets, notes
// and threats had the identical gap. Their columns are NOT NULL too, and asset
// and note validation lives in BeforeCreate ONLY by design, so the update path
// has no hook fallback at all. The 500 is already gone (ORA-01407 now
// classifies as ErrConstraint -> 400), but the dialect divergence is not: the
// same request stores '' on PostgreSQL and 400s on Oracle.

func TestAssetPatch_RejectsEmptyName(t *testing.T) {
	s := &GormAssetRepository{}
	a := &Asset{Name: "original"}

	err := s.applyPatchOperation(a, PatchOperation{
		Op: string(Replace), Path: PatchPathName, Value: "",
	})

	require.Error(t, err, "ASSETS.NAME is NOT NULL; '' must not reach the store")
	assert.Equal(t, "original", a.Name, "the rejected value must not be applied")
}

func TestAssetPatch_RejectsEmptyType(t *testing.T) {
	s := &GormAssetRepository{}
	a := &Asset{Type: AssetType("data")}

	err := s.applyPatchOperation(a, PatchOperation{
		Op: string(Replace), Path: PatchPathType, Value: "",
	})

	require.Error(t, err, "ASSETS.TYPE is NOT NULL; '' must not reach the store")
	assert.Equal(t, AssetType("data"), a.Type)
}

// Assets had no enum check at all on this path — the exact defect
// ValidateRepositoryType was re-added to close on repositories.
func TestAssetPatch_RejectsOffEnumType(t *testing.T) {
	s := &GormAssetRepository{}
	a := &Asset{Type: AssetType("data")}

	err := s.applyPatchOperation(a, PatchOperation{
		Op: string(Replace), Path: PatchPathType, Value: "not-a-real-asset-type",
	})

	require.Error(t, err, "an off-enum asset type must be rejected, not persisted")
	assert.Equal(t, AssetType("data"), a.Type)
}

func TestAssetPatch_AcceptsValidNameAndType(t *testing.T) {
	s := &GormAssetRepository{}
	a := &Asset{Name: "original", Type: AssetType("data")}

	require.NoError(t, s.applyPatchOperation(a, PatchOperation{
		Op: string(Replace), Path: PatchPathName, Value: "renamed",
	}))
	assert.Equal(t, "renamed", a.Name)
}

func TestNotePatch_RejectsEmptyName(t *testing.T) {
	s := &GormNoteRepository{}
	n := &Note{Name: "original", Content: "body"}

	err := s.applyPatchOperation(n, PatchOperation{
		Op: string(Replace), Path: PatchPathName, Value: "",
	})

	require.Error(t, err, "NOTES.NAME is NOT NULL; '' must not reach the store")
	assert.Equal(t, "original", n.Name)
}

// NOTES.CONTENT is DBText -> CLOB on Oracle, which binds ” as NULL exactly as
// VARCHAR2 does, so the CLOB type buys no safety here.
func TestNotePatch_RejectsEmptyContent(t *testing.T) {
	s := &GormNoteRepository{}
	n := &Note{Name: "note", Content: "body"}

	err := s.applyPatchOperation(n, PatchOperation{
		Op: string(Replace), Path: patchPathContent, Value: "",
	})

	require.Error(t, err, "NOTES.CONTENT is NOT NULL; '' must not reach the store")
	assert.Equal(t, "body", n.Content)
}

func TestNotePatch_RejectsWhitespaceOnlyContent(t *testing.T) {
	s := &GormNoteRepository{}
	n := &Note{Name: "note", Content: "body"}

	err := s.applyPatchOperation(n, PatchOperation{
		Op: string(Replace), Path: patchPathContent, Value: "   \t\n ",
	})

	require.Error(t, err, "whitespace-only content is empty once trimmed")
	assert.Equal(t, "body", n.Content)
}

func TestThreatPatch_RejectsEmptyName(t *testing.T) {
	s := &GormThreatRepository{}
	th := &Threat{Name: "original"}

	err := s.applyPatchOperation(th, PatchOperation{
		Op: string(Replace), Path: PatchPathName, Value: "",
	})

	require.Error(t, err, "THREATS.NAME is NOT NULL; '' must not reach the store")
	assert.Equal(t, "original", th.Name)
}

func TestThreatPatch_AcceptsValidName(t *testing.T) {
	s := &GormThreatRepository{}
	th := &Threat{Name: "original"}

	require.NoError(t, s.applyPatchOperation(th, PatchOperation{
		Op: string(Replace), Path: PatchPathName, Value: "renamed",
	}))
	assert.Equal(t, "renamed", th.Name)
}

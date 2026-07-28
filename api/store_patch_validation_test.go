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

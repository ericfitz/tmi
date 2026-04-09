package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPDFExtractor_Name(t *testing.T) {
	e := NewPDFExtractor()
	assert.Equal(t, "pdf", e.Name())
}

func TestPDFExtractor_CanHandle(t *testing.T) {
	e := NewPDFExtractor()
	assert.True(t, e.CanHandle("application/pdf"))
	assert.True(t, e.CanHandle("application/PDF"))
	assert.False(t, e.CanHandle("text/plain"))
	assert.False(t, e.CanHandle("text/html"))
}

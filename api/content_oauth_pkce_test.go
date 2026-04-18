package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPKCEVerifier_Length_43Chars(t *testing.T) {
	verifier, err := NewPKCEVerifier()
	require.NoError(t, err)
	assert.Len(t, verifier, 43)
}

func TestPKCEVerifier_Unique(t *testing.T) {
	v1, err := NewPKCEVerifier()
	require.NoError(t, err)
	v2, err := NewPKCEVerifier()
	require.NoError(t, err)
	assert.NotEqual(t, v1, v2)
}

func TestPKCES256Challenge_KnownValue(t *testing.T) {
	// RFC 7636 Appendix B test vector
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	assert.Equal(t, expected, PKCES256Challenge(verifier))
}

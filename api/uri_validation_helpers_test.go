package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const privateIPIssueURI = "https://192.168.1.1/issue"

func TestValidateURI_NilValidator(t *testing.T) {
	err := validateURI(nil, "issue_uri", "http://10.0.0.1/issue")
	assert.NoError(t, err, "nil validator should return nil")
}

func TestValidateURI_EmptyURI(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateURI(v, "issue_uri", "")
	assert.NoError(t, err, "empty URI should return nil")
}

func TestValidateURI_ValidURI(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateURI(v, "issue_uri", "https://github.com/org/repo/issues/1")
	assert.NoError(t, err, "valid public URI should pass")
}

func TestValidateURI_InvalidURI_PrivateIP(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateURI(v, "issue_uri", "https://10.0.0.1/issue")
	require.Error(t, err, "private IP should fail")
	assert.Contains(t, err.Error(), "invalid issue_uri")
}

func TestValidateOptionalURI_NilValidator(t *testing.T) {
	uri := "http://10.0.0.1/issue"
	err := validateOptionalURI(nil, "issue_uri", &uri)
	assert.NoError(t, err, "nil validator should return nil")
}

func TestValidateOptionalURI_NilPointer(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateOptionalURI(v, "issue_uri", nil)
	assert.NoError(t, err, "nil pointer should return nil")
}

func TestValidateOptionalURI_EmptyString(t *testing.T) {
	v := NewURIValidator(nil, nil)
	empty := ""
	err := validateOptionalURI(v, "issue_uri", &empty)
	assert.NoError(t, err, "empty string should return nil")
}

func TestValidateOptionalURI_ValidURI(t *testing.T) {
	v := NewURIValidator(nil, nil)
	uri := "https://github.com/org/repo/issues/1"
	err := validateOptionalURI(v, "issue_uri", &uri)
	assert.NoError(t, err, "valid public URI should pass")
}

func TestValidateOptionalURI_InvalidURI(t *testing.T) {
	v := NewURIValidator(nil, nil)
	uri := privateIPIssueURI
	err := validateOptionalURI(v, "issue_uri", &uri)
	require.Error(t, err, "private IP should fail")
	assert.Contains(t, err.Error(), "invalid issue_uri")
}

func TestValidateURIPatchOperations_NilValidator(t *testing.T) {
	ops := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: "http://10.0.0.1/issue"},
	}
	err := ValidateURIPatchOperations(nil, ops, []string{"/issue_uri"})
	assert.NoError(t, err, "nil validator should return nil")
}

func TestValidateURIPatchOperations_ValidURIField(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: "https://github.com/org/repo/issues/1"},
	}
	err := ValidateURIPatchOperations(v, ops, []string{"/issue_uri"})
	assert.NoError(t, err, "valid public URI should pass")
}

func TestValidateURIPatchOperations_InvalidURIField(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: "https://10.0.0.1/issue"},
	}
	err := ValidateURIPatchOperations(v, ops, []string{"/issue_uri"})
	require.Error(t, err, "private IP should fail")
	assert.Contains(t, err.Error(), "invalid issue_uri")
}

func TestValidateURIPatchOperations_AddOperation(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "add", Path: "/issue_uri", Value: "https://10.0.0.1/issue"},
	}
	err := ValidateURIPatchOperations(v, ops, []string{"/issue_uri"})
	require.Error(t, err, "add with private IP should fail")
	assert.Contains(t, err.Error(), "invalid issue_uri")
}

func TestValidateURIPatchOperations_IgnoresNonURIFields(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "replace", Path: "/title", Value: "http://10.0.0.1/issue"},
	}
	err := ValidateURIPatchOperations(v, ops, []string{"/issue_uri"})
	assert.NoError(t, err, "non-URI field should be ignored")
}

func TestValidateURIPatchOperations_SkipsRemoveOp(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "remove", Path: "/issue_uri"},
	}
	err := ValidateURIPatchOperations(v, ops, []string{"/issue_uri"})
	assert.NoError(t, err, "remove op should be skipped")
}

func TestValidateURIPatchOperations_SkipsTestOp(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "test", Path: "/issue_uri", Value: "https://10.0.0.1/issue"},
	}
	err := ValidateURIPatchOperations(v, ops, []string{"/issue_uri"})
	assert.NoError(t, err, "test op should be skipped")
}

func TestValidateURIPatchOperations_EmptyValue(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: ""},
	}
	err := ValidateURIPatchOperations(v, ops, []string{"/issue_uri"})
	assert.NoError(t, err, "empty value should be skipped")
}

func TestValidateURIPatchOperations_NonStringValue(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: 42},
	}
	err := ValidateURIPatchOperations(v, ops, []string{"/issue_uri"})
	assert.NoError(t, err, "non-string value should be skipped")
}

func TestValidateOptionalReferenceURI_AllowsPrivateHostname(t *testing.T) {
	v := NewURIValidator(nil, nil)
	uri := "https://jira.oraclecorp.com/OSAR-123"
	err := validateOptionalReferenceURI(v, "issue_uri", &uri)
	assert.NoError(t, err, "reference-mode validator must accept hosts that resolve to private IPs")
}

func TestValidateOptionalReferenceURI_AllowsLiteralPrivateIP(t *testing.T) {
	v := NewURIValidator(nil, nil)
	uri := privateIPIssueURI
	err := validateOptionalReferenceURI(v, "issue_uri", &uri)
	assert.NoError(t, err, "reference-mode validator must accept literal private IPs (no fetch happens)")
}

func TestValidateOptionalReferenceURI_RejectsBadScheme(t *testing.T) {
	v := NewURIValidator(nil, nil)
	uri := "ftp://example.com/issue"
	err := validateOptionalReferenceURI(v, "issue_uri", &uri)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported scheme")
}

func TestValidateOptionalReferenceURI_RejectsMalformedURL(t *testing.T) {
	v := NewURIValidator(nil, nil)
	uri := "not a url"
	err := validateOptionalReferenceURI(v, "issue_uri", &uri)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid issue_uri")
}

func TestValidateOptionalReferenceURI_RespectsAllowlist(t *testing.T) {
	v := NewURIValidator([]string{"jira.example.com"}, nil)
	good := "https://jira.example.com/OSAR-1"
	bad := "https://other.example.com/OSAR-1"
	assert.NoError(t, validateOptionalReferenceURI(v, "issue_uri", &good))
	err := validateOptionalReferenceURI(v, "issue_uri", &bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in allowlist")
}

func TestValidateOptionalReferenceURI_NilValidator(t *testing.T) {
	uri := privateIPIssueURI
	err := validateOptionalReferenceURI(nil, "issue_uri", &uri)
	assert.NoError(t, err)
}

func TestValidateOptionalReferenceURI_NilPointer(t *testing.T) {
	v := NewURIValidator(nil, nil)
	err := validateOptionalReferenceURI(v, "issue_uri", nil)
	assert.NoError(t, err)
}

func TestValidateReferenceURIPatchOperations_AllowsPrivateHostname(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "add", Path: "/issue_uri", Value: "https://jira.oraclecorp.com/OSAR-123"},
	}
	err := ValidateReferenceURIPatchOperations(v, ops, []string{"/issue_uri"})
	assert.NoError(t, err)
}

func TestValidateReferenceURIPatchOperations_RejectsBadScheme(t *testing.T) {
	v := NewURIValidator(nil, nil)
	ops := []PatchOperation{
		{Op: "replace", Path: "/issue_uri", Value: "ftp://example.com/x"},
	}
	err := ValidateReferenceURIPatchOperations(v, ops, []string{"/issue_uri"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported scheme")
}

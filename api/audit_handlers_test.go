package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAuditFilters_NoParams(t *testing.T) {
	// When no query parameters are provided, buildAuditFilters should return
	// a non-nil empty filter struct (not nil), ensuring the query returns all
	// audit entries without any filtering. This is a regression test for #171.
	filters := buildAuditFilters(nil, nil, nil, nil, nil)
	require.NotNil(t, filters, "buildAuditFilters should return non-nil even when no params are provided")
	assert.Nil(t, filters.ObjectType, "ObjectType should be nil when not specified")
	assert.Nil(t, filters.ChangeType, "ChangeType should be nil when not specified")
	assert.Nil(t, filters.ActorEmail, "ActorEmail should be nil when not specified")
	assert.Nil(t, filters.After, "After should be nil when not specified")
	assert.Nil(t, filters.Before, "Before should be nil when not specified")
}

func TestBuildAuditFilters_WithObjectType(t *testing.T) {
	objectType := GetThreatModelAuditTrailParamsObjectTypeThreatModel
	filters := buildAuditFilters(&objectType, nil, nil, nil, nil)
	require.NotNil(t, filters)
	require.NotNil(t, filters.ObjectType)
	assert.Equal(t, "threat_model", *filters.ObjectType)
}

func TestBuildAuditFilters_WithChangeType(t *testing.T) {
	changeType := Created
	filters := buildAuditFilters(nil, &changeType, nil, nil, nil)
	require.NotNil(t, filters)
	require.NotNil(t, filters.ChangeType)
	assert.Equal(t, "created", *filters.ChangeType)
}

func TestBuildAuditFilters_WithActorEmail(t *testing.T) {
	email := AuditActorEmail("alice@example.com")
	filters := buildAuditFilters(nil, nil, &email, nil, nil)
	require.NotNil(t, filters)
	require.NotNil(t, filters.ActorEmail)
	assert.Equal(t, "alice@example.com", *filters.ActorEmail)
}

func TestBuildAuditFilters_WithTimeRange(t *testing.T) {
	after := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	filters := buildAuditFilters(nil, nil, nil, &after, &before)
	require.NotNil(t, filters)
	require.NotNil(t, filters.After)
	require.NotNil(t, filters.Before)
	assert.Equal(t, after, *filters.After)
	assert.Equal(t, before, *filters.Before)
}

func TestBuildAuditFilters_EmptyStringIgnored(t *testing.T) {
	// Empty string query parameter values should be treated as absent
	emptyObjectType := GetThreatModelAuditTrailParamsObjectType("")
	emptyChangeType := GetThreatModelAuditTrailParamsChangeType("")
	emptyEmail := AuditActorEmail("")

	filters := buildAuditFilters(&emptyObjectType, &emptyChangeType, &emptyEmail, nil, nil)
	require.NotNil(t, filters)
	assert.Nil(t, filters.ObjectType, "empty string object_type should be treated as absent")
	assert.Nil(t, filters.ChangeType, "empty string change_type should be treated as absent")
	assert.Nil(t, filters.ActorEmail, "empty string actor_email should be treated as absent")
}

func TestBuildAuditFilters_AllParams(t *testing.T) {
	objectType := GetThreatModelAuditTrailParamsObjectTypeDiagram
	changeType := Updated
	email := AuditActorEmail("bob@example.com")
	after := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)

	filters := buildAuditFilters(&objectType, &changeType, &email, &after, &before)
	require.NotNil(t, filters)
	require.NotNil(t, filters.ObjectType)
	assert.Equal(t, "diagram", *filters.ObjectType)
	require.NotNil(t, filters.ChangeType)
	assert.Equal(t, "updated", *filters.ChangeType)
	require.NotNil(t, filters.ActorEmail)
	assert.Equal(t, "bob@example.com", *filters.ActorEmail)
	require.NotNil(t, filters.After)
	require.NotNil(t, filters.Before)
}

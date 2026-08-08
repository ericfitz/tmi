package main

import (
	"reflect"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

func TestNullableStringColumn_BackfillSQL(t *testing.T) {
	c := nullableStringColumn{Table: "teams", Column: "description"}
	assert.Equal(t, `UPDATE teams SET description = NULL WHERE description = ''`, c.backfillSQL())
}

func TestIsNullableStringField(t *testing.T) {
	assert.True(t, isNullableStringField(reflect.TypeOf(models.NullableDBVarchar{})), "NullableDBVarchar should match")
	assert.True(t, isNullableStringField(reflect.TypeOf(models.NullableDBText{})), "NullableDBText should match")

	assert.False(t, isNullableStringField(reflect.TypeOf(models.DBVarchar(""))), "non-nullable DBVarchar should not match")
	assert.False(t, isNullableStringField(reflect.TypeOf(models.DBText(""))), "non-nullable DBText should not match")
	assert.False(t, isNullableStringField(reflect.TypeOf("")), "plain string should not match")
}

// TestEnumerateNullableStringColumns_FindsKnownNullableColumns pins that the
// mechanical model walk finds columns known to be NullableDBVarchar/
// NullableDBText, using PostgreSQL (lowercase) naming.
func TestEnumerateNullableStringColumns_FindsKnownNullableColumns(t *testing.T) {
	pairs, err := enumerateNullableStringColumns(schema.NamingStrategy{})
	require.NoError(t, err)
	require.NotEmpty(t, pairs)

	want := []nullableStringColumn{
		{Table: "teams", Column: "description"},
		{Table: "teams", Column: "uri"},
		{Table: "teams", Column: "email_address"},
		{Table: "threat_models", Column: "description"},
		{Table: "threat_models", Column: "issue_uri"},
		{Table: "group_members", Column: "user_internal_uuid"},
		{Table: "documents", Column: "picker_provider_id"},
	}

	got := make(map[nullableStringColumn]bool, len(pairs))
	for _, p := range pairs {
		got[p] = true
	}

	for _, w := range want {
		assert.True(t, got[w], "expected enumeration to include %s.%s", w.Table, w.Column)
	}
}

// TestEnumerateNullableStringColumns_ExcludesNonNullableColumns pins that
// columns backed by the non-nullable DBVarchar/DBText types (which never
// need the empty-to-NULL backfill) are not included.
func TestEnumerateNullableStringColumns_ExcludesNonNullableColumns(t *testing.T) {
	pairs, err := enumerateNullableStringColumns(schema.NamingStrategy{})
	require.NoError(t, err)

	got := make(map[nullableStringColumn]bool, len(pairs))
	for _, p := range pairs {
		got[p] = true
	}

	notWant := []nullableStringColumn{
		{Table: "teams", Column: "name"},           // DBVarchar, not null
		{Table: "documents", Column: "uri"},        // DBText, not null
		{Table: "group_members", Column: "id"},     // primary key
		{Table: "threat_models", Column: "status"}, // DBVarchar, not null
	}

	for _, nw := range notWant {
		assert.False(t, got[nw], "did not expect enumeration to include non-nullable column %s.%s", nw.Table, nw.Column)
	}
}

// TestEnumerateNullableStringColumns_NoDuplicatePairs pins that each model is
// visited exactly once, so the same (table, column) is never emitted twice.
func TestEnumerateNullableStringColumns_NoDuplicatePairs(t *testing.T) {
	pairs, err := enumerateNullableStringColumns(schema.NamingStrategy{})
	require.NoError(t, err)

	seen := make(map[nullableStringColumn]bool, len(pairs))
	for _, p := range pairs {
		require.False(t, seen[p], "duplicate pair %s.%s", p.Table, p.Column)
		seen[p] = true
	}
}

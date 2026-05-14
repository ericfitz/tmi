package models

import (
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func TestDBVarchar_GormDBDataType_Postgres(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectPostgres}
	field := &schema.Field{Size: 256}
	got := DBVarchar("").GormDBDataType(db, field)
	assert.Equal(t, "varchar(256)", got, "GormDBDataType(postgres, size=256)")
}

func TestDBVarchar_GormDBDataType_Oracle(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectOracle}
	field := &schema.Field{Size: 256}
	got := DBVarchar("").GormDBDataType(db, field)
	assert.Equal(t, "varchar2(256 char)", got, "GormDBDataType(oracle, size=256)")
}

func TestDBVarchar_GormDBDataType_DefaultSizeWhenUnset(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectOracle}
	field := &schema.Field{Size: 0}
	got := DBVarchar("").GormDBDataType(db, field)
	assert.Equal(t, "varchar2(255 char)", got, "GormDBDataType(oracle, size=0) default 255")
}

func TestDBVarchar_GormDBDataType_DefaultSizeWhenUnset_Postgres(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectPostgres}
	field := &schema.Field{Size: 0}
	got := DBVarchar("").GormDBDataType(db, field)
	assert.Equal(t, "varchar(255)", got, "GormDBDataType(postgres, size=0) default 255")
}

func TestDBVarchar_Scan(t *testing.T) {
	var v DBVarchar
	require.NoError(t, v.Scan("hello"), "Scan(string)")
	assert.Equal(t, "hello", string(v), "Scan(string) value")

	v = ""
	require.NoError(t, v.Scan([]byte("world")), "Scan([]byte)")
	assert.Equal(t, "world", string(v), "Scan([]byte) value")

	v = "stale"
	require.NoError(t, v.Scan(nil), "Scan(nil)")
	assert.Equal(t, "", string(v), "Scan(nil) should yield empty string")
}

func TestDBVarchar_Scan_UnsupportedType(t *testing.T) {
	var v DBVarchar
	err := v.Scan(42)
	require.Error(t, err, "Scan(int) should return an error")
}

func TestDBVarchar_Value(t *testing.T) {
	v := DBVarchar("hello")
	got, err := v.Value()
	require.NoError(t, err, "Value()")
	assert.Equal(t, driver.Value("hello"), got, "Value()")
}

func TestNullableDBVarchar_GormDBDataType_Postgres(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectPostgres}
	field := &schema.Field{Size: 128}
	got := NullableDBVarchar{}.GormDBDataType(db, field)
	assert.Equal(t, "varchar(128)", got, "NullableDBVarchar GormDBDataType(postgres, size=128)")
}

func TestNullableDBVarchar_GormDBDataType_Oracle(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectOracle}
	field := &schema.Field{Size: 128}
	got := NullableDBVarchar{}.GormDBDataType(db, field)
	assert.Equal(t, "varchar2(128 char)", got, "NullableDBVarchar GormDBDataType(oracle, size=128)")
}

func TestNullableDBVarchar_ValidScan(t *testing.T) {
	var v NullableDBVarchar
	require.NoError(t, v.Scan("hello"), "Scan(string)")
	assert.True(t, v.Valid, "Valid should be true after Scan(string)")
	assert.Equal(t, "hello", v.String, "String value after Scan(string)")
}

func TestNullableDBVarchar_NilScan(t *testing.T) {
	v := NullableDBVarchar{String: "stale", Valid: true}
	require.NoError(t, v.Scan(nil), "Scan(nil)")
	assert.False(t, v.Valid, "Valid should be false after Scan(nil)")
	assert.Equal(t, "", v.String, "String should be empty after Scan(nil)")
}

func TestNullableDBVarchar_Scan_UnsupportedType(t *testing.T) {
	var v NullableDBVarchar
	err := v.Scan(42)
	require.Error(t, err, "Scan(int) should return an error")
}

func TestNullableDBVarchar_Value(t *testing.T) {
	v := NullableDBVarchar{String: "hi", Valid: true}
	got, err := v.Value()
	require.NoError(t, err, "Value(valid)")
	assert.Equal(t, driver.Value("hi"), got, "Value(valid)")

	v = NullableDBVarchar{Valid: false}
	got, err = v.Value()
	require.NoError(t, err, "Value(invalid)")
	assert.Nil(t, got, "Value(invalid) should be nil")
}

func TestNullableDBVarchar_Ptr(t *testing.T) {
	v := NullableDBVarchar{String: "hi", Valid: true}
	p := v.Ptr()
	require.NotNil(t, p, "Ptr(valid) should not be nil")
	assert.Equal(t, "hi", *p, "Ptr(valid) value")

	v = NullableDBVarchar{Valid: false}
	p = v.Ptr()
	assert.Nil(t, p, "Ptr(invalid) should be nil")
}

func TestNewNullableDBVarchar(t *testing.T) {
	s := "hello"
	v := NewNullableDBVarchar(&s)
	assert.True(t, v.Valid, "NewNullableDBVarchar(&s) should be valid")
	assert.Equal(t, "hello", v.String, "NewNullableDBVarchar(&s) string value")

	v = NewNullableDBVarchar(nil)
	assert.False(t, v.Valid, "NewNullableDBVarchar(nil) should not be valid")
	assert.Equal(t, "", v.String, "NewNullableDBVarchar(nil) string should be empty")
}

// mockDialector is a minimal gorm.Dialector that only implements Name() for tests.
type mockDialector struct{ name string }

func (m mockDialector) Name() string                                                { return m.name }
func (m mockDialector) Initialize(*gorm.DB) error                                   { return nil }
func (m mockDialector) Migrator(*gorm.DB) gorm.Migrator                             { return nil }
func (m mockDialector) DataTypeOf(*schema.Field) string                             { return "" }
func (m mockDialector) DefaultValueOf(*schema.Field) clause.Expression              { return nil }
func (m mockDialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v any) {}
func (m mockDialector) QuoteTo(clause.Writer, string)                               {}
func (m mockDialector) Explain(sql string, vars ...any) string                      { return sql }

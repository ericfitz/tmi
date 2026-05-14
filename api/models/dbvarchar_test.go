package models

import (
	"database/sql/driver"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func TestDBVarchar_GormDBDataType_Postgres(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectPostgres}
	field := &schema.Field{Size: 256}
	got := DBVarchar("").GormDBDataType(db, field)
	want := "varchar(256)"
	if got != want {
		t.Fatalf("GormDBDataType(postgres, size=256) = %q, want %q", got, want)
	}
}

func TestDBVarchar_GormDBDataType_Oracle(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectOracle}
	field := &schema.Field{Size: 256}
	got := DBVarchar("").GormDBDataType(db, field)
	want := "varchar2(256 char)"
	if got != want {
		t.Fatalf("GormDBDataType(oracle, size=256) = %q, want %q", got, want)
	}
}

func TestDBVarchar_GormDBDataType_DefaultSizeWhenUnset(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{}}
	db.Dialector = mockDialector{name: dialectOracle}
	field := &schema.Field{Size: 0}
	got := DBVarchar("").GormDBDataType(db, field)
	want := "varchar2(255 char)"
	if got != want {
		t.Fatalf("GormDBDataType(oracle, size=0) = %q, want %q (default 255)", got, want)
	}
}

func TestDBVarchar_Scan(t *testing.T) {
	var v DBVarchar
	if err := v.Scan("hello"); err != nil {
		t.Fatalf("Scan(string): %v", err)
	}
	if string(v) != "hello" {
		t.Fatalf("Scan(string): got %q, want %q", string(v), "hello")
	}
	v = ""
	if err := v.Scan([]byte("world")); err != nil {
		t.Fatalf("Scan([]byte): %v", err)
	}
	if string(v) != "world" {
		t.Fatalf("Scan([]byte): got %q, want %q", string(v), "world")
	}
	v = "stale"
	if err := v.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if string(v) != "" {
		t.Fatalf("Scan(nil): got %q, want empty", string(v))
	}
}

func TestDBVarchar_Value(t *testing.T) {
	v := DBVarchar("hello")
	got, err := v.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got != driver.Value("hello") {
		t.Fatalf("Value: got %v, want %q", got, "hello")
	}
}

func TestNullableDBVarchar_ValidScan(t *testing.T) {
	var v NullableDBVarchar
	if err := v.Scan("hello"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !v.Valid || v.String != "hello" {
		t.Fatalf("Scan(string): got {%q, %v}, want {%q, true}", v.String, v.Valid, "hello")
	}
}

func TestNullableDBVarchar_NilScan(t *testing.T) {
	v := NullableDBVarchar{String: "stale", Valid: true}
	if err := v.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if v.Valid || v.String != "" {
		t.Fatalf("Scan(nil): got {%q, %v}, want {\"\", false}", v.String, v.Valid)
	}
}

func TestNullableDBVarchar_Value(t *testing.T) {
	v := NullableDBVarchar{String: "hi", Valid: true}
	got, err := v.Value()
	if err != nil {
		t.Fatalf("Value(valid): %v", err)
	}
	if got != driver.Value("hi") {
		t.Fatalf("Value(valid): got %v, want %q", got, "hi")
	}

	v = NullableDBVarchar{Valid: false}
	got, err = v.Value()
	if err != nil {
		t.Fatalf("Value(invalid): %v", err)
	}
	if got != nil {
		t.Fatalf("Value(invalid): got %v, want nil", got)
	}
}

func TestNullableDBVarchar_Ptr(t *testing.T) {
	v := NullableDBVarchar{String: "hi", Valid: true}
	p := v.Ptr()
	if p == nil || *p != "hi" {
		t.Fatalf("Ptr(valid): got %v, want pointer to %q", p, "hi")
	}

	v = NullableDBVarchar{Valid: false}
	p = v.Ptr()
	if p != nil {
		t.Fatalf("Ptr(invalid): got %v, want nil", p)
	}
}

func TestNewNullableDBVarchar(t *testing.T) {
	s := "hello"
	v := NewNullableDBVarchar(&s)
	if !v.Valid || v.String != "hello" {
		t.Fatalf("NewNullableDBVarchar(&\"hello\"): got {%q, %v}", v.String, v.Valid)
	}

	v = NewNullableDBVarchar(nil)
	if v.Valid || v.String != "" {
		t.Fatalf("NewNullableDBVarchar(nil): got {%q, %v}", v.String, v.Valid)
	}
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

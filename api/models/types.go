package models

import (
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Dialect constants for database type detection
const (
	dialectPostgres  = "postgres"
	dialectOracle    = "oracle"
	dialectMySQL     = "mysql"
	dialectSQLServer = "sqlserver"
	dialectSQLite    = "sqlite"
)

// Database column type constants for cross-database compatibility
const (
	dbTypeText         = "TEXT"
	dbTypeCLOB         = "CLOB"
	dbTypeLongText     = "LONGTEXT"
	dbTypeNVarcharMax  = "NVARCHAR(MAX)"
	dbTypeJSONB        = "JSONB"
	dbTypeJSON         = "JSON"
	dbTypeBoolean      = "BOOLEAN"
	dbTypeBytea        = "BYTEA"
	dbTypeBLOB         = "BLOB"
	dbTypeLongBLOB     = "LONGBLOB"
	dbTypeVarBinaryMax = "VARBINARY(MAX)"
)

// StringArray is a custom type that stores string arrays as JSON
// This outputs JSON array format ["val1","val2"] which works for both
// PostgreSQL JSONB columns and Oracle JSON columns
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: database-compatible string slice that serializes as a JSON array for cross-dialect storage (pure)
type StringArray []string

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific column type for StringArray storage (pure)
func (StringArray) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeText
	case dialectOracle:
		return dbTypeCLOB
	case dialectMySQL:
		return dbTypeLongText
	case dialectSQLServer:
		return dbTypeNVarcharMax
	case dialectSQLite:
		return dbTypeText
	default:
		return dbTypeText
	}
}

// Value implements the driver.Valuer interface for database writes
// Outputs JSON array format: ["val1","val2","val3"]
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize a StringArray to a JSON array string for database writes (pure)
func (a StringArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "[]", nil
	}
	bytes, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return string(bytes), nil
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a StringArray from a JSON or PostgreSQL array format database value (pure)
func (a *StringArray) Scan(value any) error {
	if value == nil {
		*a = []string{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into StringArray", value)
	}

	// Handle empty values
	if len(bytes) == 0 || string(bytes) == "{}" || string(bytes) == "[]" {
		*a = []string{}
		return nil
	}

	// Handle PostgreSQL array format {val1,val2,val3}
	if len(bytes) > 0 && bytes[0] == '{' {
		// Convert PostgreSQL array format to JSON array
		s := string(bytes)
		// Remove braces
		s = s[1 : len(s)-1]
		if s == "" {
			*a = []string{}
			return nil
		}
		// Split by comma and build JSON array
		// Note: This is a simplified parser that doesn't handle escaped commas
		var result []string
		if s != "" {
			// Handle PostgreSQL's quote escaping
			inQuote := false
			current := ""
			for i := 0; i < len(s); i++ {
				c := s[i]
				switch {
				case c == '"':
					inQuote = !inQuote
				case c == ',' && !inQuote:
					result = append(result, current)
					current = ""
				default:
					current += string(c)
				}
			}
			if current != "" {
				result = append(result, current)
			}
		}
		*a = result
		return nil
	}

	// Handle JSON array format
	return json.Unmarshal(bytes, a)
}

// CVSSScore represents a CVSS vector and score pair for threat assessment
// SEM@dafcb0b707b3aec36d55d6377ccb7a5a04b9dff7: CVSS vector string and numeric score pair for threat severity assessment (pure)
type CVSSScore struct {
	Vector string  `json:"vector"`
	Score  float64 `json:"score"`
}

// CVSSArray is a custom type that stores CVSS score arrays as JSON
// This outputs JSON array format [{"vector":"...","score":9.8}] which works for both
// PostgreSQL JSONB columns and Oracle JSON columns
// SEM@dafcb0b707b3aec36d55d6377ccb7a5a04b9dff7: database-compatible slice of CVSS scores serialized as a JSON array for cross-dialect storage (pure)
type CVSSArray []CVSSScore

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific column type for CVSSArray storage (pure)
func (CVSSArray) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeText
	case dialectOracle:
		return dbTypeCLOB
	case dialectMySQL:
		return dbTypeLongText
	case dialectSQLServer:
		return dbTypeNVarcharMax
	case dialectSQLite:
		return dbTypeText
	default:
		return dbTypeText
	}
}

// Value implements the driver.Valuer interface for database writes
// Outputs JSON array format: [{"vector":"...","score":9.8}]
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize a CVSSArray to a JSON array string for database writes (pure)
func (a CVSSArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "[]", nil
	}
	bytes, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return string(bytes), nil
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a CVSSArray from a JSON array database value (pure)
func (a *CVSSArray) Scan(value any) error {
	if value == nil {
		*a = []CVSSScore{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into CVSSArray", value)
	}

	// Handle empty values
	if len(bytes) == 0 || string(bytes) == "[]" {
		*a = []CVSSScore{}
		return nil
	}

	// Handle JSON array format
	return json.Unmarshal(bytes, a)
}

// SSVCScore represents an SSVC (Stakeholder-Specific Vulnerability Categorization) assessment result
// SEM@41c4d4fee1a1b990b10999bf34a8957796b3a0ce: SSVC stakeholder vulnerability categorization result with vector, decision, and methodology (pure)
type SSVCScore struct {
	Vector      string `json:"vector"`
	Decision    string `json:"decision"`
	Methodology string `json:"methodology"`
}

// NullableSSVC is a custom type that stores an optional SSVC score as JSON
// SEM@41c4d4fee1a1b990b10999bf34a8957796b3a0ce: nullable wrapper for an SSVCScore that distinguishes SQL NULL from a zero value (pure)
type NullableSSVC struct {
	SSVCScore
	Valid bool `json:"-"` // false means NULL in the database
}

// GormDBDataType implements the GormDBDataTypeInterface
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific column type for NullableSSVC storage (pure)
func (NullableSSVC) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeText
	case dialectOracle:
		return dbTypeCLOB
	case dialectMySQL:
		return dbTypeLongText
	case dialectSQLServer:
		return dbTypeNVarcharMax
	case dialectSQLite:
		return dbTypeText
	default:
		return dbTypeText
	}
}

// Value implements the driver.Valuer interface for database writes
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize a NullableSSVC to JSON or NULL for database writes (pure)
func (s NullableSSVC) Value() (driver.Value, error) {
	if !s.Valid {
		return nil, nil
	}
	bytes, err := json.Marshal(s.SSVCScore)
	if err != nil {
		return nil, err
	}
	return string(bytes), nil
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a NullableSSVC from a JSON database value, setting Valid=false for NULL (pure)
func (s *NullableSSVC) Scan(value any) error {
	if value == nil {
		s.Valid = false
		s.SSVCScore = SSVCScore{}
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into NullableSSVC", value)
	}

	if len(bytes) == 0 {
		s.Valid = false
		s.SSVCScore = SSVCScore{}
		return nil
	}

	if err := json.Unmarshal(bytes, &s.SSVCScore); err != nil {
		return err
	}
	s.Valid = true
	return nil
}

// MarshalJSON implements the json.Marshaler interface.
// A valid NullableSSVC marshals as the inner SSVCScore JSON object; an invalid
// one marshals as null. This mirrors the Value/Scan database representation so
// JSON round-trips (e.g. through a Redis cache) match the on-disk encoding.
// SEM@7e4b34fec28c8e0d5c235301e7a132b24212d3a9: serialize a NullableSSVC as a JSON object or null, mirroring its database encoding (pure)
func (s NullableSSVC) MarshalJSON() ([]byte, error) {
	if !s.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(s.SSVCScore)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// A JSON null sets Valid to false; any other value is unmarshaled into the
// inner SSVCScore and sets Valid to true.
// SEM@7e4b34fec28c8e0d5c235301e7a132b24212d3a9: deserialize a NullableSSVC from JSON null or an SSVCScore object (pure)
func (s *NullableSSVC) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		s.SSVCScore = SSVCScore{}
		s.Valid = false
		return nil
	}
	if err := json.Unmarshal(data, &s.SSVCScore); err != nil {
		return err
	}
	s.Valid = true
	return nil
}

// JSONMap is a custom type that stores JSON objects
// This works across both PostgreSQL JSONB and Oracle JSON
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: database-compatible map for JSON objects stored as JSONB or CLOB across dialects (pure)
type JSONMap map[string]any

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific column type for JSONMap storage (pure)
func (JSONMap) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeJSONB
	case dialectOracle:
		return dbTypeCLOB
	case dialectMySQL:
		return dbTypeJSON
	case dialectSQLServer:
		return dbTypeNVarcharMax
	case dialectSQLite:
		return dbTypeText
	default:
		return dbTypeText
	}
}

// Value implements the driver.Valuer interface for database writes
// Returns string (not []byte) for Oracle CLOB compatibility
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize a JSONMap to a JSON object string for database writes (pure)
func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	bytes, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(bytes), nil
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a JSONMap from a JSON string database value (pure)
func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = make(map[string]any)
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into JSONMap", value)
	}

	if len(bytes) == 0 {
		*m = make(map[string]any)
		return nil
	}

	return json.Unmarshal(bytes, m)
}

// JSONRaw is a custom type for storing raw JSON (like cells in diagrams)
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: database-compatible raw JSON value stored as JSONB or CLOB across dialects (pure)
type JSONRaw json.RawMessage

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific column type for JSONRaw storage (pure)
func (JSONRaw) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeJSONB
	case dialectOracle:
		return dbTypeCLOB
	case dialectMySQL:
		return dbTypeJSON
	case dialectSQLServer:
		return dbTypeNVarcharMax
	case dialectSQLite:
		return dbTypeText
	default:
		return dbTypeText
	}
}

// Value implements the driver.Valuer interface for database writes
// Returns string (not []byte) for Oracle CLOB compatibility.
// A zero-length non-nil slice is normalized to nil so that behavior is
// consistent across PostgreSQL JSONB (which would reject "" as 22P02) and
// Oracle CLOB (which silently coerces "" to NULL).
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize JSONRaw to a driver string, normalizing empty to NULL (pure)
func (j JSONRaw) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a DB value into JSONRaw, decoding Oracle hex-encoded CLOB when needed (pure)
func (j *JSONRaw) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}

	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into JSONRaw", value)
	}

	// Oracle may return CLOB/BLOB data as uppercase hex-encoded strings
	// (e.g., "7B7D" instead of "{}"). Detect this by checking if the data
	// is valid JSON first; if not, try hex decoding.
	if len(raw) >= 2 && len(raw)%2 == 0 && !json.Valid(raw) {
		if decoded, err := hex.DecodeString(string(raw)); err == nil {
			raw = decoded
		}
	}

	*j = raw
	return nil
}

// MarshalJSON implements json.Marshaler
// SEM@7e4b34fec28c8e0d5c235301e7a132b24212d3a9: serialize JSONRaw to JSON, emitting null for nil (pure)
func (j JSONRaw) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON implements json.Unmarshaler
// SEM@7e4b34fec28c8e0d5c235301e7a132b24212d3a9: deserialize JSON bytes into JSONRaw (pure)
func (j *JSONRaw) UnmarshalJSON(data []byte) error {
	if j == nil {
		return fmt.Errorf("JSONRaw: UnmarshalJSON on nil pointer")
	}
	*j = append((*j)[0:0], data...)
	return nil
}

// DBText is a cross-database large text type.
// Uses TEXT on PostgreSQL, CLOB on Oracle, LONGTEXT on MySQL,
// NVARCHAR(MAX) on SQL Server, and TEXT on SQLite.
// SEM@ae4222674aa836756d4e53ad513582d70f862bae: cross-database large text type mapping to TEXT, CLOB, or dialect equivalent (pure)
type DBText string

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific large text column type for DBText (pure)
func (DBText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeText
	case dialectOracle:
		return dbTypeCLOB
	case dialectMySQL:
		return dbTypeLongText
	case dialectSQLServer:
		return dbTypeNVarcharMax
	case dialectSQLite:
		return dbTypeText
	default:
		return dbTypeText
	}
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a DB value into DBText, accepting bytes or string (pure)
func (t *DBText) Scan(value any) error {
	if value == nil {
		*t = ""
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*t = DBText(v)
	case string:
		*t = DBText(v)
	default:
		return fmt.Errorf("cannot scan type %T into DBText", value)
	}
	return nil
}

// Value implements the driver.Valuer interface for database writes
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize DBText to a driver string value (pure)
func (t DBText) Value() (driver.Value, error) {
	return string(t), nil
}

// String returns the underlying string value
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: return the underlying string value of DBText (pure)
func (t DBText) String() string {
	return string(t)
}

// NullableDBText is a nullable cross-database large text type.
// Wraps a string with a Valid flag for NULL handling.
// Uses TEXT on PostgreSQL, CLOB on Oracle, LONGTEXT on MySQL,
// NVARCHAR(MAX) on SQL Server, and TEXT on SQLite.
// SEM@ae4222674aa836756d4e53ad513582d70f862bae: nullable cross-database large text type with Valid flag for NULL handling (pure)
type NullableDBText struct {
	String string
	Valid  bool
}

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific large text column type for NullableDBText (pure)
func (NullableDBText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeText
	case dialectOracle:
		return dbTypeCLOB
	case dialectMySQL:
		return dbTypeLongText
	case dialectSQLServer:
		return dbTypeNVarcharMax
	case dialectSQLite:
		return dbTypeText
	default:
		return dbTypeText
	}
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a DB value into NullableDBText, setting Valid false for NULL (pure)
func (t *NullableDBText) Scan(value any) error {
	if value == nil {
		t.String, t.Valid = "", false
		return nil
	}
	t.Valid = true
	switch v := value.(type) {
	case []byte:
		t.String = string(v)
	case string:
		t.String = v
	default:
		return fmt.Errorf("cannot scan type %T into NullableDBText", value)
	}
	return nil
}

// Value implements the driver.Valuer interface for database writes
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize NullableDBText to a driver string or NULL (pure)
func (t NullableDBText) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return t.String, nil
}

// Ptr returns a pointer to the string, or nil if not valid
// SEM@ae4222674aa836756d4e53ad513582d70f862bae: return a string pointer from NullableDBText, or nil if not valid (pure)
func (t NullableDBText) Ptr() *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

// MarshalJSON implements the json.Marshaler interface.
// A valid NullableDBText marshals as a JSON string; an invalid one marshals as null.
// SEM@7e4b34fec28c8e0d5c235301e7a132b24212d3a9: serialize NullableDBText to a JSON string or null (pure)
func (t NullableDBText) MarshalJSON() ([]byte, error) {
	if !t.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(t.String)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// A JSON null sets Valid to false; a JSON string sets Valid to true and String to the value.
// SEM@7e4b34fec28c8e0d5c235301e7a132b24212d3a9: deserialize JSON string or null into NullableDBText (pure)
func (t *NullableDBText) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		t.String, t.Valid = "", false
		return nil
	}
	if err := json.Unmarshal(data, &t.String); err != nil {
		return err
	}
	t.Valid = true
	return nil
}

// NewNullableDBText creates a NullableDBText from a string pointer
// SEM@ae4222674aa836756d4e53ad513582d70f862bae: build a NullableDBText from a string pointer, setting Valid false for nil (pure)
func NewNullableDBText(s *string) NullableDBText {
	if s == nil {
		return NullableDBText{Valid: false}
	}
	return NullableDBText{String: *s, Valid: true}
}

// DBVarchar is a cross-database length-bounded text type with CHAR semantics.
// Uses varchar(N) on PostgreSQL (already char-counted), varchar2(N CHAR) on
// Oracle (avoiding default BYTE semantics under AL32UTF8), varchar(N) on
// MySQL (utf8mb4 is char-counted), nvarchar(N) on SQL Server, varchar(N) on
// SQLite. The length N is carried by the GORM `size:` tag, not by the Go type.
// SEM@ae4222674aa836756d4e53ad513582d70f862bae: cross-database length-bounded text type with CHAR semantics per dialect (pure)
type DBVarchar string

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// The column size is read from field.Size (populated from the `size:` GORM tag).
// A field.Size of 0 falls back to 255 as a safety default; every usage site
// should set size: explicitly.
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific bounded varchar column type using the GORM size tag (pure)
func (DBVarchar) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	n := field.Size
	if n <= 0 {
		n = 255
	}
	switch db.Name() {
	case dialectPostgres:
		return fmt.Sprintf("varchar(%d)", n)
	case dialectOracle:
		return fmt.Sprintf("varchar2(%d char)", n)
	case dialectMySQL:
		return fmt.Sprintf("varchar(%d)", n)
	case dialectSQLServer:
		return fmt.Sprintf("nvarchar(%d)", n)
	case dialectSQLite:
		return fmt.Sprintf("varchar(%d)", n)
	default:
		return fmt.Sprintf("varchar(%d)", n)
	}
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a DB value into DBVarchar, accepting bytes or string (pure)
func (v *DBVarchar) Scan(value any) error {
	if value == nil {
		*v = ""
		return nil
	}
	switch s := value.(type) {
	case []byte:
		*v = DBVarchar(s)
	case string:
		*v = DBVarchar(s)
	default:
		return fmt.Errorf("cannot scan type %T into DBVarchar", value)
	}
	return nil
}

// Value implements the driver.Valuer interface for database writes
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize DBVarchar to a driver string value (pure)
func (v DBVarchar) Value() (driver.Value, error) {
	return string(v), nil
}

// String returns the underlying string value
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: return the underlying string value of DBVarchar (pure)
func (v DBVarchar) String() string {
	return string(v)
}

// NullableDBVarchar is a nullable cross-database length-bounded text type with
// CHAR semantics. Wraps a string with a Valid flag for NULL handling.
// Maps to the same column types as DBVarchar per dialect.
// SEM@ae4222674aa836756d4e53ad513582d70f862bae: nullable cross-database bounded varchar type with Valid flag for NULL handling (pure)
type NullableDBVarchar struct {
	String string
	Valid  bool
}

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific bounded varchar column type for NullableDBVarchar (pure)
func (NullableDBVarchar) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	n := field.Size
	if n <= 0 {
		n = 255
	}
	switch db.Name() {
	case dialectPostgres:
		return fmt.Sprintf("varchar(%d)", n)
	case dialectOracle:
		return fmt.Sprintf("varchar2(%d char)", n)
	case dialectMySQL:
		return fmt.Sprintf("varchar(%d)", n)
	case dialectSQLServer:
		return fmt.Sprintf("nvarchar(%d)", n)
	case dialectSQLite:
		return fmt.Sprintf("varchar(%d)", n)
	default:
		return fmt.Sprintf("varchar(%d)", n)
	}
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a DB value into NullableDBVarchar, setting Valid false for NULL (pure)
func (v *NullableDBVarchar) Scan(value any) error {
	if value == nil {
		v.String, v.Valid = "", false
		return nil
	}
	v.Valid = true
	switch s := value.(type) {
	case []byte:
		v.String = string(s)
	case string:
		v.String = s
	default:
		return fmt.Errorf("cannot scan type %T into NullableDBVarchar", value)
	}
	return nil
}

// Value implements the driver.Valuer interface for database writes
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize NullableDBVarchar to a driver string or NULL (pure)
func (v NullableDBVarchar) Value() (driver.Value, error) {
	if !v.Valid {
		return nil, nil
	}
	return v.String, nil
}

// Ptr returns a pointer to the string, or nil if not valid
// SEM@ae4222674aa836756d4e53ad513582d70f862bae: return a string pointer from NullableDBVarchar, or nil if not valid (pure)
func (v NullableDBVarchar) Ptr() *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// NewNullableDBVarchar creates a NullableDBVarchar from a string pointer
// SEM@10f24b09b53917610ab8bdc25c0c5f8621dcc0ee: build a NullableDBVarchar from a string pointer, setting Valid false for nil (pure)
func NewNullableDBVarchar(s *string) NullableDBVarchar {
	if s == nil {
		return NullableDBVarchar{Valid: false}
	}
	return NullableDBVarchar{String: *s, Valid: true}
}

// MarshalJSON implements the json.Marshaler interface.
// A valid NullableDBVarchar marshals as a JSON string; an invalid one marshals as null.
// SEM@7e4b34fec28c8e0d5c235301e7a132b24212d3a9: serialize NullableDBVarchar to a JSON string or null (pure)
func (v NullableDBVarchar) MarshalJSON() ([]byte, error) {
	if !v.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(v.String)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// A JSON null sets Valid to false; a JSON string sets Valid to true and String to the value.
// SEM@7e4b34fec28c8e0d5c235301e7a132b24212d3a9: deserialize JSON string or null into NullableDBVarchar (pure)
func (v *NullableDBVarchar) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		v.String, v.Valid = "", false
		return nil
	}
	if err := json.Unmarshal(data, &v.String); err != nil {
		return err
	}
	v.Valid = true
	return nil
}

// DBBool is a cross-database boolean type that handles different database
// representations of booleans. Oracle uses NUMBER(1), MySQL uses TINYINT(1),
// SQL Server uses BIT, while PostgreSQL and SQLite have native boolean support.
// This type implements sql.Scanner and driver.Valuer to handle the conversion
// for all supported databases.
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: cross-database boolean type handling NUMBER(1), TINYINT, BIT, and native bool per dialect (pure)
type DBBool bool

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific boolean column type for DBBool (pure)
func (DBBool) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeBoolean
	case dialectOracle:
		return "NUMBER(1)"
	case dialectMySQL:
		return "TINYINT(1)"
	case dialectSQLServer:
		return "BIT"
	case dialectSQLite:
		return "INTEGER"
	default:
		return dbTypeBoolean
	}
}

// Scan implements the sql.Scanner interface for DBBool.
// It handles:
// - bool (PostgreSQL native boolean)
// - int64/int/int32 (numeric representation)
// - godror.Number (Oracle's numeric type, implements fmt.Stringer)
// - nil (NULL values)
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a DB value into DBBool, handling numeric, native bool, and Oracle godror.Number (pure)
func (b *DBBool) Scan(value any) error {
	if value == nil {
		*b = false
		return nil
	}

	switch v := value.(type) {
	case bool:
		*b = DBBool(v)
	case int64:
		*b = v != 0
	case int:
		*b = v != 0
	case int32:
		*b = v != 0
	case float64:
		*b = v != 0
	default:
		// Handle godror.Number which implements fmt.Stringer
		if stringer, ok := value.(fmt.Stringer); ok {
			str := stringer.String()
			*b = str != "0" && str != ""
		} else {
			return fmt.Errorf("cannot scan type %T into DBBool", value)
		}
	}
	return nil
}

// Value implements the driver.Valuer interface for DBBool.
// It returns the boolean as a native Go bool for cross-database compatibility.
// PostgreSQL expects bool for boolean columns, and Oracle's godror driver
// can handle Go bool and convert it to NUMBER(1) appropriately.
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize DBBool to a native Go bool for cross-database driver compatibility (pure)
func (b DBBool) Value() (driver.Value, error) {
	return bool(b), nil
}

// Bool returns the underlying bool value.
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: return the underlying bool value of DBBool (pure)
func (b DBBool) Bool() bool {
	return bool(b)
}

// DBBytes is a cross-database binary data type.
// Uses BYTEA on PostgreSQL, BLOB on Oracle, LONGBLOB on MySQL,
// VARBINARY(MAX) on SQL Server, and BLOB on SQLite.
// SEM@d60ddcf3f407dda0e558c55c1c597317e13eece1: cross-database binary data type mapping to BYTEA, BLOB, or dialect equivalent (pure)
type DBBytes []byte

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: return the dialect-specific binary column type for DBBytes (pure)
func (DBBytes) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return dbTypeBytea
	case dialectOracle:
		return dbTypeBLOB
	case dialectMySQL:
		return dbTypeLongBLOB
	case dialectSQLServer:
		return dbTypeVarBinaryMax
	case dialectSQLite:
		return dbTypeBLOB
	default:
		return dbTypeBLOB
	}
}

// Scan implements the sql.Scanner interface for database reads
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: deserialize a DB byte slice into DBBytes (pure)
func (b *DBBytes) Scan(value any) error {
	if value == nil {
		*b = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*b = v
	default:
		return fmt.Errorf("cannot scan type %T into DBBytes", value)
	}
	return nil
}

// Value implements the driver.Valuer interface for database writes
// SEM@55d98405ac043c7929d10873466d6f6f3ebc53e8: serialize DBBytes to a driver byte slice, returning NULL for nil (pure)
func (b DBBytes) Value() (driver.Value, error) {
	if b == nil {
		return nil, nil
	}
	return []byte(b), nil
}

// OracleBool is an alias for DBBool for backward compatibility.
//
// Deprecated: Use DBBool instead.
type OracleBool = DBBool

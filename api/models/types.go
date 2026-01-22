package models

import (
	"database/sql/driver"
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

// StringArray is a custom type that stores string arrays as JSON
// This outputs JSON array format ["val1","val2"] which works for both
// PostgreSQL JSONB columns and Oracle JSON columns
type StringArray []string

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
func (StringArray) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return "TEXT"
	case dialectOracle:
		return "CLOB"
	case dialectMySQL:
		return "LONGTEXT"
	case dialectSQLServer:
		return "NVARCHAR(MAX)"
	case dialectSQLite:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// Value implements the driver.Valuer interface for database writes
// Outputs JSON array format: ["val1","val2","val3"]
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
func (a *StringArray) Scan(value interface{}) error {
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
				if c == '"' {
					inQuote = !inQuote
				} else if c == ',' && !inQuote {
					result = append(result, current)
					current = ""
				} else {
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
type CVSSScore struct {
	Vector string  `json:"vector"`
	Score  float64 `json:"score"`
}

// CVSSArray is a custom type that stores CVSS score arrays as JSON
// This outputs JSON array format [{"vector":"...","score":9.8}] which works for both
// PostgreSQL JSONB columns and Oracle JSON columns
type CVSSArray []CVSSScore

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
func (CVSSArray) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return "TEXT"
	case dialectOracle:
		return "CLOB"
	case dialectMySQL:
		return "LONGTEXT"
	case dialectSQLServer:
		return "NVARCHAR(MAX)"
	case dialectSQLite:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// Value implements the driver.Valuer interface for database writes
// Outputs JSON array format: [{"vector":"...","score":9.8}]
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
func (a *CVSSArray) Scan(value interface{}) error {
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

// JSONMap is a custom type that stores JSON objects
// This works across both PostgreSQL JSONB and Oracle JSON
type JSONMap map[string]interface{}

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
func (JSONMap) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return "JSONB"
	case dialectOracle:
		return "CLOB"
	case dialectMySQL:
		return "JSON"
	case dialectSQLServer:
		return "NVARCHAR(MAX)"
	case dialectSQLite:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// Value implements the driver.Valuer interface for database writes
// Returns string (not []byte) for Oracle CLOB compatibility
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
func (m *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*m = make(map[string]interface{})
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
		*m = make(map[string]interface{})
		return nil
	}

	return json.Unmarshal(bytes, m)
}

// JSONRaw is a custom type for storing raw JSON (like cells in diagrams)
type JSONRaw json.RawMessage

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
func (JSONRaw) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return "JSONB"
	case dialectOracle:
		return "CLOB"
	case dialectMySQL:
		return "JSON"
	case dialectSQLServer:
		return "NVARCHAR(MAX)"
	case dialectSQLite:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// Value implements the driver.Valuer interface for database writes
// Returns string (not []byte) for Oracle CLOB compatibility
func (j JSONRaw) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return string(j), nil
}

// Scan implements the sql.Scanner interface for database reads
func (j *JSONRaw) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	switch v := value.(type) {
	case []byte:
		*j = v
	case string:
		*j = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into JSONRaw", value)
	}
	return nil
}

// MarshalJSON implements json.Marshaler
func (j JSONRaw) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON implements json.Unmarshaler
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
type DBText string

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
func (DBText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return "TEXT"
	case dialectOracle:
		return "CLOB"
	case dialectMySQL:
		return "LONGTEXT"
	case dialectSQLServer:
		return "NVARCHAR(MAX)"
	case dialectSQLite:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// Scan implements the sql.Scanner interface for database reads
func (t *DBText) Scan(value interface{}) error {
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
func (t DBText) Value() (driver.Value, error) {
	return string(t), nil
}

// String returns the underlying string value
func (t DBText) String() string {
	return string(t)
}

// NullableDBText is a nullable cross-database large text type.
// Wraps a string with a Valid flag for NULL handling.
// Uses TEXT on PostgreSQL, CLOB on Oracle, LONGTEXT on MySQL,
// NVARCHAR(MAX) on SQL Server, and TEXT on SQLite.
type NullableDBText struct {
	String string
	Valid  bool
}

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
func (NullableDBText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return "TEXT"
	case dialectOracle:
		return "CLOB"
	case dialectMySQL:
		return "LONGTEXT"
	case dialectSQLServer:
		return "NVARCHAR(MAX)"
	case dialectSQLite:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// Scan implements the sql.Scanner interface for database reads
func (t *NullableDBText) Scan(value interface{}) error {
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
func (t NullableDBText) Value() (driver.Value, error) {
	if !t.Valid {
		return nil, nil
	}
	return t.String, nil
}

// Ptr returns a pointer to the string, or nil if not valid
func (t NullableDBText) Ptr() *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

// NewNullableDBText creates a NullableDBText from a string pointer
func NewNullableDBText(s *string) NullableDBText {
	if s == nil {
		return NullableDBText{Valid: false}
	}
	return NullableDBText{String: *s, Valid: true}
}

// DBBool is a cross-database boolean type that handles different database
// representations of booleans. Oracle uses NUMBER(1), MySQL uses TINYINT(1),
// SQL Server uses BIT, while PostgreSQL and SQLite have native boolean support.
// This type implements sql.Scanner and driver.Valuer to handle the conversion
// for all supported databases.
type DBBool bool

// GormDBDataType implements the GormDBDataTypeInterface to return
// dialect-specific column types for cross-database compatibility
func (DBBool) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case dialectPostgres:
		return "BOOLEAN"
	case dialectOracle:
		return "NUMBER(1)"
	case dialectMySQL:
		return "TINYINT(1)"
	case dialectSQLServer:
		return "BIT"
	case dialectSQLite:
		return "INTEGER"
	default:
		return "BOOLEAN"
	}
}

// Scan implements the sql.Scanner interface for DBBool.
// It handles:
// - bool (PostgreSQL native boolean)
// - int64/int/int32 (numeric representation)
// - godror.Number (Oracle's numeric type, implements fmt.Stringer)
// - nil (NULL values)
func (b *DBBool) Scan(value interface{}) error {
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
func (b DBBool) Value() (driver.Value, error) {
	return bool(b), nil
}

// Bool returns the underlying bool value.
func (b DBBool) Bool() bool {
	return bool(b)
}

// OracleBool is an alias for DBBool for backward compatibility.
// Deprecated: Use DBBool instead.
type OracleBool = DBBool

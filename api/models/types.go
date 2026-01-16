package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// StringArray is a custom type that stores string arrays
// For PostgreSQL, this outputs native array format {val1,val2}
// For other databases (Oracle, MySQL, etc.), this outputs JSON format ["val1","val2"]
type StringArray []string

// Value implements the driver.Valuer interface for database writes
// Outputs PostgreSQL array literal format: {val1,val2,val3}
func (a StringArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}

	// Build PostgreSQL array literal format: {val1,val2,val3}
	// Values containing commas, quotes, or braces need to be quoted
	result := "{"
	for i, v := range a {
		if i > 0 {
			result += ","
		}
		// Quote the value if it contains special characters
		needsQuoting := false
		for _, c := range v {
			if c == ',' || c == '"' || c == '{' || c == '}' || c == '\\' || c == ' ' {
				needsQuoting = true
				break
			}
		}
		if needsQuoting {
			// Escape backslashes and quotes, then wrap in quotes
			escaped := ""
			for _, c := range v {
				if c == '\\' || c == '"' {
					escaped += "\\"
				}
				escaped += string(c)
			}
			result += "\"" + escaped + "\""
		} else {
			result += v
		}
	}
	result += "}"
	return result, nil
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
	if len(bytes) == 0 || string(bytes) == "{}" {
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

// JSONMap is a custom type that stores JSON objects
// This works across both PostgreSQL JSONB and Oracle JSON
type JSONMap map[string]interface{}

// Value implements the driver.Valuer interface for database writes
func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
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

// Value implements the driver.Valuer interface for database writes
func (j JSONRaw) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return []byte(j), nil
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

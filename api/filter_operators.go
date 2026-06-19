package api

import (
	"fmt"
	"strings"
)

// FilterOperator represents the type of filter operation to apply.
// SEM@40d9903ef472b183a4ed2bcf0478562c757f255d: enum of filter operators supported in query parameter values (pure)
type FilterOperator int

const (
	// FilterOpNone indicates a plain value with no operator prefix.
	FilterOpNone FilterOperator = iota
	// FilterOpIsNull indicates the field should be NULL.
	FilterOpIsNull
	// FilterOpIsNotNull indicates the field should be NOT NULL.
	FilterOpIsNotNull
)

// ParsedFilter holds the result of parsing a filter query parameter value.
// SEM@40d9903ef472b183a4ed2bcf0478562c757f255d: parsed filter holding an operator type and an optional plain value (pure)
type ParsedFilter struct {
	Operator FilterOperator
	Value    string // Empty for is:null/is:notnull, populated for plain values
}

// maxOperatorPrefixLen is the maximum length of an operator prefix before the colon.
// Prefixes longer than this are treated as plain values (e.g., "user:name@example.com").
const maxOperatorPrefixLen = 3

// supportedOperatorPrefixes lists the recognized operator prefixes.
// Only values starting with one of these prefixes (case-insensitive) are parsed as operators.
var supportedOperatorPrefixes = []string{"is:"}

// ParseFilterValue parses a query parameter value for operator prefixes.
// Recognized operators: is:null, is:notnull.
// Unrecognized operators return a 400 RequestError.
// Values without a recognized operator prefix are returned as plain values.
// SEM@40d9903ef472b183a4ed2bcf0478562c757f255d: parse a filter query parameter value into an operator and value, rejecting unknown operators (pure)
func ParseFilterValue(paramName, rawValue string) (ParsedFilter, error) {
	if rawValue == "" {
		return ParsedFilter{Operator: FilterOpNone, Value: ""}, nil
	}

	lower := strings.ToLower(rawValue)

	// Check if the value starts with a known operator prefix
	for _, prefix := range supportedOperatorPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return parseOperator(paramName, rawValue, prefix, lower)
		}
	}

	// Check if the value looks like an unsupported operator prefix (short alpha string before colon)
	if idx := strings.Index(lower, ":"); idx > 0 && idx <= maxOperatorPrefixLen && isAllAlpha(lower[:idx]) {
		prefix := lower[:idx]
		return ParsedFilter{}, InvalidInputError(
			fmt.Sprintf("Unsupported filter operator prefix %q for parameter %q. Supported prefixes: is:", prefix, paramName))
	}

	// No operator prefix — treat as plain value
	return ParsedFilter{Operator: FilterOpNone, Value: rawValue}, nil
}

// isAllAlpha returns true if the string contains only ASCII alphabetic characters.
// SEM@40d9903ef472b183a4ed2bcf0478562c757f255d: check whether a string contains only lowercase ASCII letters (pure)
func isAllAlpha(s string) bool {
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return len(s) > 0
}

// parseOperator parses the operand after a recognized operator prefix.
// paramName is used for error messages, rawValue is the original input,
// prefix is the matched operator prefix (e.g., "is:"), and lower is the lowercased rawValue.
// SEM@5b38b9a109d5e10e1a9a58a35a692f19c30a0ed5: parse the operand after a recognized filter operator prefix into a ParsedFilter (pure)
func parseOperator(paramName, rawValue, prefix, lower string) (ParsedFilter, error) {
	operand := lower[len(prefix):]

	switch prefix {
	case "is:":
		switch operand {
		case jsonNull:
			return ParsedFilter{Operator: FilterOpIsNull}, nil
		case "notnull":
			return ParsedFilter{Operator: FilterOpIsNotNull}, nil
		case "":
			return ParsedFilter{}, InvalidInputError(
				fmt.Sprintf("Incomplete filter operator for parameter %q: %q. Supported: is:null, is:notnull", paramName, rawValue))
		default:
			return ParsedFilter{}, InvalidInputError(
				fmt.Sprintf("Unsupported filter operator for parameter %q: %q. Supported: is:null, is:notnull", paramName, rawValue))
		}
	default:
		return ParsedFilter{}, InvalidInputError(
			fmt.Sprintf("Unsupported filter operator prefix %q for parameter %q. Supported prefixes: is:", prefix, paramName))
	}
}

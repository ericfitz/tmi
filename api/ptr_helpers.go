package api

import "time"

// strPtr converts a string to a pointer, returning nil for empty strings.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// strPtrOrEmpty converts a string to a pointer, including empty strings.
//
//go:fix inline
func strPtrOrEmpty(s string) *string {
	return new(s)
}

// strFromPtr converts a string pointer to a string, returning "" for nil.
func strFromPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// timePtr returns the time pointer as-is (for consistent API response building).
func timePtr(t *time.Time) *time.Time {
	return t
}

// timeFromPtr returns the time pointer as-is (for consistent API request reading).
func timeFromPtr(t *time.Time) *time.Time {
	return t
}

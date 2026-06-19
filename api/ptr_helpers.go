package api

import "time"

// strPtr converts a string to a pointer, returning nil for empty strings.
// SEM@d297fe4b5f55988308f2ad4d355485c9cbc7988e: convert a string to a pointer, returning nil for the empty string (pure)
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// strFromPtr converts a string pointer to a string, returning "" for nil.
// SEM@d297fe4b5f55988308f2ad4d355485c9cbc7988e: convert a string pointer to a string, returning empty string for nil (pure)
func strFromPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// timePtr returns the time pointer as-is (for consistent API response building).
// SEM@d297fe4b5f55988308f2ad4d355485c9cbc7988e: return a time pointer unchanged for consistent API response building (pure)
func timePtr(t *time.Time) *time.Time {
	return t
}

// timeFromPtr returns the time pointer as-is (for consistent API request reading).
// SEM@d297fe4b5f55988308f2ad4d355485c9cbc7988e: return a time pointer unchanged for consistent API request reading (pure)
func timeFromPtr(t *time.Time) *time.Time {
	return t
}

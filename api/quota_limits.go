package api

import "fmt"

// Quota ceiling constants define maximum allowed values for various quota types
// These limits prevent integer overflow and ensure system stability
const (
	// User API Quota limits
	MaxRequestsPerMinute = 10000  // Maximum API requests per minute per user
	MaxRequestsPerHour   = 600000 // Maximum API requests per hour per user

	// Webhook Quota limits
	MaxSubscriptions                 = 100   // Maximum webhook subscriptions per user
	MaxEventsPerMinute               = 1000  // Maximum webhook events per minute
	MaxSubscriptionRequestsPerMinute = 100   // Maximum subscription requests per minute
	MaxSubscriptionRequestsPerDay    = 10000 // Maximum subscription requests per day

	// Addon Invocation Quota limits
	MaxActiveInvocations  = 10   // Maximum concurrent active addon invocations
	MaxInvocationsPerHour = 1000 // Maximum addon invocations per hour
)

// ValidateQuotaValue validates that a quota value is within acceptable bounds
func ValidateQuotaValue(value int, min int, max int, fieldName string) error {
	if value < min {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s must be at least %d (got %d)", fieldName, min, value),
		}
	}
	if value > max {
		return &RequestError{
			Status:  400,
			Code:    "invalid_input",
			Message: fmt.Sprintf("%s exceeds maximum allowed value of %d (got %d)", fieldName, max, value),
		}
	}
	return nil
}

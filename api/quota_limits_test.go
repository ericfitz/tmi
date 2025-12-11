package api

import (
	"testing"
)

func TestValidateQuotaValue(t *testing.T) {
	tests := []struct {
		name      string
		value     int
		min       int
		max       int
		fieldName string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "value within bounds",
			value:     50,
			min:       1,
			max:       100,
			fieldName: "max_requests",
			wantErr:   false,
		},
		{
			name:      "value at minimum bound",
			value:     1,
			min:       1,
			max:       100,
			fieldName: "max_requests",
			wantErr:   false,
		},
		{
			name:      "value at maximum bound",
			value:     100,
			min:       1,
			max:       100,
			fieldName: "max_requests",
			wantErr:   false,
		},
		{
			name:      "value below minimum",
			value:     0,
			min:       1,
			max:       100,
			fieldName: "max_requests",
			wantErr:   true,
			errMsg:    "must be at least 1",
		},
		{
			name:      "value above maximum",
			value:     101,
			min:       1,
			max:       100,
			fieldName: "max_requests",
			wantErr:   true,
			errMsg:    "exceeds maximum allowed value of 100",
		},
		{
			name:      "large value within bounds",
			value:     5000,
			min:       1,
			max:       10000,
			fieldName: "max_requests_per_minute",
			wantErr:   false,
		},
		{
			name:      "integer overflow attempt (max int64)",
			value:     9223372036854775807,
			min:       1,
			max:       10000,
			fieldName: "max_requests_per_minute",
			wantErr:   true,
			errMsg:    "exceeds maximum allowed value",
		},
		{
			name:      "negative value",
			value:     -1,
			min:       1,
			max:       100,
			fieldName: "max_subscriptions",
			wantErr:   true,
			errMsg:    "must be at least 1",
		},
		{
			name:      "very large min value rejected",
			value:     50,
			min:       100,
			max:       200,
			fieldName: "max_value",
			wantErr:   true,
			errMsg:    "must be at least 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuotaValue(tt.value, tt.min, tt.max, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateQuotaValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				reqErr, ok := err.(*RequestError)
				if !ok {
					t.Errorf("Expected RequestError, got %T", err)
					return
				}
				if !containsString(reqErr.Message, tt.errMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errMsg, reqErr.Message)
				}
			}
		})
	}
}

func TestQuotaConstants(t *testing.T) {
	// Test that quota constants are sensible
	tests := []struct {
		name     string
		constant int
		minValue int
		maxValue int
	}{
		{
			name:     "MaxRequestsPerMinute",
			constant: MaxRequestsPerMinute,
			minValue: 100,
			maxValue: 100000,
		},
		{
			name:     "MaxRequestsPerHour",
			constant: MaxRequestsPerHour,
			minValue: 1000,
			maxValue: 10000000,
		},
		{
			name:     "MaxSubscriptions",
			constant: MaxSubscriptions,
			minValue: 1,
			maxValue: 1000,
		},
		{
			name:     "MaxEventsPerMinute",
			constant: MaxEventsPerMinute,
			minValue: 10,
			maxValue: 100000,
		},
		{
			name:     "MaxSubscriptionRequestsPerMinute",
			constant: MaxSubscriptionRequestsPerMinute,
			minValue: 1,
			maxValue: 10000,
		},
		{
			name:     "MaxSubscriptionRequestsPerDay",
			constant: MaxSubscriptionRequestsPerDay,
			minValue: 10,
			maxValue: 1000000,
		},
		{
			name:     "MaxActiveInvocations",
			constant: MaxActiveInvocations,
			minValue: 1,
			maxValue: 100,
		},
		{
			name:     "MaxInvocationsPerHour",
			constant: MaxInvocationsPerHour,
			minValue: 10,
			maxValue: 100000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant < tt.minValue {
				t.Errorf("%s = %d is too low (minimum should be %d)", tt.name, tt.constant, tt.minValue)
			}
			if tt.constant > tt.maxValue {
				t.Errorf("%s = %d is too high (maximum should be %d)", tt.name, tt.constant, tt.maxValue)
			}
		})
	}
}

func TestUserAPIQuotaBounds(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{
			name:    "valid requests per minute",
			value:   1000,
			wantErr: false,
		},
		{
			name:    "max requests per minute",
			value:   MaxRequestsPerMinute,
			wantErr: false,
		},
		{
			name:    "exceeds max requests per minute",
			value:   MaxRequestsPerMinute + 1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuotaValue(tt.value, 1, MaxRequestsPerMinute, "max_requests_per_minute")
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateQuotaValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWebhookQuotaBounds(t *testing.T) {
	tests := []struct {
		name      string
		quota     int
		constant  int
		fieldName string
		wantErr   bool
	}{
		{
			name:      "valid subscriptions",
			quota:     10,
			constant:  MaxSubscriptions,
			fieldName: "max_subscriptions",
			wantErr:   false,
		},
		{
			name:      "max subscriptions",
			quota:     MaxSubscriptions,
			constant:  MaxSubscriptions,
			fieldName: "max_subscriptions",
			wantErr:   false,
		},
		{
			name:      "exceeds max subscriptions",
			quota:     MaxSubscriptions + 1,
			constant:  MaxSubscriptions,
			fieldName: "max_subscriptions",
			wantErr:   true,
		},
		{
			name:      "valid events per minute",
			quota:     100,
			constant:  MaxEventsPerMinute,
			fieldName: "max_events_per_minute",
			wantErr:   false,
		},
		{
			name:      "exceeds events per minute",
			quota:     MaxEventsPerMinute + 1,
			constant:  MaxEventsPerMinute,
			fieldName: "max_events_per_minute",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuotaValue(tt.quota, 1, tt.constant, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateQuotaValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddonInvocationQuotaBounds(t *testing.T) {
	tests := []struct {
		name      string
		quota     int
		constant  int
		fieldName string
		wantErr   bool
	}{
		{
			name:      "valid active invocations",
			quota:     5,
			constant:  MaxActiveInvocations,
			fieldName: "max_active_invocations",
			wantErr:   false,
		},
		{
			name:      "max active invocations",
			quota:     MaxActiveInvocations,
			constant:  MaxActiveInvocations,
			fieldName: "max_active_invocations",
			wantErr:   false,
		},
		{
			name:      "exceeds max active invocations",
			quota:     MaxActiveInvocations + 1,
			constant:  MaxActiveInvocations,
			fieldName: "max_active_invocations",
			wantErr:   true,
		},
		{
			name:      "valid invocations per hour",
			quota:     500,
			constant:  MaxInvocationsPerHour,
			fieldName: "max_invocations_per_hour",
			wantErr:   false,
		},
		{
			name:      "exceeds invocations per hour",
			quota:     MaxInvocationsPerHour + 1,
			constant:  MaxInvocationsPerHour,
			fieldName: "max_invocations_per_hour",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQuotaValue(tt.quota, 1, tt.constant, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateQuotaValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

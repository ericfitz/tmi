package config

import (
	"testing"
)

func TestIsTestMode(t *testing.T) {
	// Create a config with IsTest set to false
	config := &Config{
		Logging: LoggingConfig{
			IsTest: false,
		},
	}

	// Should return true because we're running under 'go test'
	if !config.IsTestMode() {
		t.Error("Expected IsTestMode() to return true when running under 'go test'")
	}

	// Test explicit test flag
	config.Logging.IsTest = true
	if !config.IsTestMode() {
		t.Error("Expected IsTestMode() to return true when IsTest is explicitly set")
	}
}

func TestIsRunningInTest(t *testing.T) {
	// This should return true when running under 'go test'
	if !isRunningInTest() {
		t.Error("Expected isRunningInTest() to return true when running under 'go test'")
	}
}
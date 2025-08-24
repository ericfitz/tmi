//go:build !dev && !test

package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetDefaultProviderID_Production(t *testing.T) {
	// Test that getDefaultProviderID returns empty string in production builds
	defaultProvider := getDefaultProviderID()
	assert.Equal(t, "", defaultProvider, "getDefaultProviderID should return empty string in production builds")
}

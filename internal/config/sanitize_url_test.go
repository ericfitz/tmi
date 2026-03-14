package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeURL_PostgresWithPassword(t *testing.T) {
	result := sanitizeURL("postgres://tmi_dev:dev123@localhost:5432/tmi_dev?sslmode=disable")
	assert.Equal(t, "postgres://tmi_dev:****@localhost:5432/tmi_dev?sslmode=disable", result)
}

func TestSanitizeURL_RedisWithPassword(t *testing.T) {
	result := sanitizeURL("redis://:secretpass@redis.example.com:6379/0")
	assert.Equal(t, "redis://:****@redis.example.com:6379/0", result)
}

func TestSanitizeURL_NoPassword(t *testing.T) {
	result := sanitizeURL("postgres://tmi_dev@localhost:5432/tmi_dev")
	assert.Equal(t, "postgres://tmi_dev@localhost:5432/tmi_dev", result)
}

func TestSanitizeURL_NoUserInfo(t *testing.T) {
	result := sanitizeURL("postgres://localhost:5432/tmi_dev")
	assert.Equal(t, "postgres://localhost:5432/tmi_dev", result)
}

func TestSanitizeURL_EmptyString(t *testing.T) {
	result := sanitizeURL("")
	assert.Equal(t, "", result)
}

func TestSanitizeURL_BareHostPort(t *testing.T) {
	result := sanitizeURL("myhost:6379")
	assert.Equal(t, "myhost:6379", result)
}

func TestSanitizeURL_InvalidURL(t *testing.T) {
	result := sanitizeURL("://bad-url")
	assert.Equal(t, "<invalid URL>", result)
}

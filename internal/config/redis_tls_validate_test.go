package config

import (
	"strings"
	"testing"
)

func TestValidate_RedisTLSNeedsCAFile(t *testing.T) {
	c := &Config{Database: DatabaseConfig{
		URL:   "postgres://u:p@localhost:5432/db",
		Redis: RedisConfig{Host: "localhost", Port: "6379", TLSEnabled: true},
	}}
	err := c.validateDatabase()
	if err == nil || !strings.Contains(err.Error(), "TMI_REDIS_TLS_CA_FILE") {
		t.Fatalf("want CA-file validation error, got %v", err)
	}
	c.Database.Redis.TLSCAFile = "/etc/tmi-redis-tls/ca.crt"
	if err := c.validateDatabase(); err != nil {
		t.Fatalf("valid TLS config rejected: %v", err)
	}

	u := &Config{Database: DatabaseConfig{
		URL:   "postgres://u:p@localhost:5432/db",
		Redis: RedisConfig{URL: "rediss://redis.example:6380"},
	}}
	if err := u.validateDatabase(); err == nil || !strings.Contains(err.Error(), "TMI_REDIS_TLS_CA_FILE") {
		t.Fatalf("rediss:// URL without CA file must be rejected, got %v", err)
	}
}

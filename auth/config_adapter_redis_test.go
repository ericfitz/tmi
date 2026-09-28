package auth

import (
	"testing"

	"github.com/ericfitz/tmi/internal/config"
)

func TestConfigFromUnified_ForwardsRedisTLS(t *testing.T) {
	u := &config.Config{}
	u.Database.Redis = config.RedisConfig{Host: "redis", Port: "6379", TLSEnabled: true, TLSCAFile: "/ca.crt"}
	ac := ConfigFromUnified(u)
	rc := ac.ToRedisConfig()
	if !rc.TLSEnabled || rc.TLSCAFile != "/ca.crt" {
		t.Fatalf("TLS fields not forwarded: %+v", rc)
	}
}

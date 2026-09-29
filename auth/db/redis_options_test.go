package db

import (
	"path/filepath"
	"testing"

	"github.com/ericfitz/tmi/internal/tlsconfig/testpki"
)

func TestRedisOptions_PlaintextByDefault(t *testing.T) {
	opts, err := redisOptions(RedisConfig{Host: "localhost", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.TLSConfig != nil {
		t.Fatal("TLS must be off unless enabled")
	}
	if opts.Addr != "localhost:6379" {
		t.Fatalf("Addr = %q", opts.Addr)
	}
}

func TestRedisOptions_TLSWithoutCAFails(t *testing.T) {
	if _, err := redisOptions(RedisConfig{Host: "redis", Port: "6379", TLSEnabled: true}); err == nil {
		t.Fatal("TLS enabled without a CA file must be an error, never a system-pool fallback")
	}
}

func TestRedisOptions_TLSPinsCAAndServerName(t *testing.T) {
	p, err := testpki.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts, err := redisOptions(RedisConfig{Host: "redis", Port: "6379", TLSEnabled: true, TLSCAFile: filepath.Join(p.Dir, "ca.crt")})
	if err != nil {
		t.Fatal(err)
	}
	if opts.TLSConfig == nil || opts.TLSConfig.RootCAs == nil {
		t.Fatal("TLSConfig with pinned RootCAs expected")
	}
	if opts.TLSConfig.ServerName != "redis" {
		t.Fatalf("ServerName = %q, want the configured host", opts.TLSConfig.ServerName)
	}
}

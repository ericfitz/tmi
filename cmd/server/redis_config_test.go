package main

import (
	"testing"

	"github.com/ericfitz/tmi/internal/config"
)

func TestBuildRedisConfig_PassesTLSFieldsThrough(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Redis = config.RedisConfig{Host: "redis", Port: "6379", TLSEnabled: true, TLSCAFile: "/etc/tmi-redis-tls/ca.crt"}
	rc := buildRedisConfig(cfg)
	if !rc.TLSEnabled || rc.TLSCAFile != "/etc/tmi-redis-tls/ca.crt" {
		t.Fatalf("TLS fields not passed through: %+v", rc)
	}
}

func TestBuildRedisConfig_RedissURLEnablesTLS(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Redis = config.RedisConfig{URL: "rediss://:pw@redis.example:6380/2", TLSCAFile: "/ca.crt"}
	rc := buildRedisConfig(cfg)
	if !rc.TLSEnabled {
		t.Fatal("rediss:// URL must enable TLS")
	}
	if rc.Host != "redis.example" || rc.Port != "6380" || rc.Password != "pw" || rc.DB != 2 {
		t.Fatalf("URL fields not parsed: %+v", rc)
	}
	cfg.Database.Redis.URL = "redis://redis.example:6379"
	if rc := buildRedisConfig(cfg); rc.TLSEnabled {
		t.Fatal("redis:// URL must not enable TLS on its own")
	}
}

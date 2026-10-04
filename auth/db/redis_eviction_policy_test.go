package db

import (
	"os"
	"regexp"
	"testing"
)

// SEM@9750a568b8ffb60cfd241d61263b7e23f990899e: test that Redis deployment files pin the noeviction memory policy so revocations are never evicted
func TestRedisEvictionPolicyIsNoeviction(t *testing.T) {
	// #1008: any evicting policy can drop blacklist:token:* entries and
	// silently re-validate revoked tokens.
	files := map[string]*regexp.Regexp{
		"../../deployments/k8s/dev/redis.yml": regexp.MustCompile(`(?s)"--maxmemory-policy"\s*-\s*"([^"]+)"`),
		"../../Dockerfile.redis":              regexp.MustCompile(`"--maxmemory-policy",\s*"([^"]+)"`),
	}
	for path, re := range files {
		b, err := os.ReadFile(path) // #nosec G304 -- fixed repo paths
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		m := re.FindSubmatch(b)
		if m == nil {
			t.Errorf("%s: no --maxmemory-policy set; pin it explicitly", path)
			continue
		}
		if string(m[1]) != "noeviction" {
			t.Errorf("%s: maxmemory-policy = %q, want noeviction (#1008)", path, m[1])
		}
	}
}

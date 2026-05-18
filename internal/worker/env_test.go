package worker

import (
	"testing"
	"time"
)

func TestEnvOr(t *testing.T) {
	t.Setenv("TMI_TEST_KEY", "hello")
	if got := EnvOr("TMI_TEST_KEY", "fallback"); got != "hello" {
		t.Fatalf("EnvOr: got %q", got)
	}
	if got := EnvOr("TMI_TEST_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("EnvOr fallback: got %q", got)
	}
}

func TestMustEnv(t *testing.T) {
	t.Setenv("TMI_TEST_REQUIRED", "v")
	if got, err := MustEnv("TMI_TEST_REQUIRED"); err != nil || got != "v" {
		t.Fatalf("MustEnv: got %q err %v", got, err)
	}
	if _, err := MustEnv("TMI_TEST_ABSENT"); err == nil {
		t.Fatal("MustEnv: expected error for absent key")
	}
}

func TestEnvDuration(t *testing.T) {
	t.Setenv("TMI_TEST_DUR", "45s")
	if got := EnvDuration("TMI_TEST_DUR", time.Minute); got != 45*time.Second {
		t.Fatalf("EnvDuration: got %v", got)
	}
	if got := EnvDuration("TMI_TEST_DUR_MISSING", time.Minute); got != time.Minute {
		t.Fatalf("EnvDuration fallback: got %v", got)
	}
	t.Setenv("TMI_TEST_DUR_BAD", "not-a-duration")
	if got := EnvDuration("TMI_TEST_DUR_BAD", time.Minute); got != time.Minute {
		t.Fatalf("EnvDuration bad-value fallback: got %v", got)
	}
}

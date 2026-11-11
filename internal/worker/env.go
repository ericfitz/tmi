package worker

import (
	"fmt"
	"os"
	"time"
)

// MustEnv returns the value of key or an error if it is unset/empty.
func MustEnv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("worker: required env var %s is not set", key)
	}
	return v, nil
}

// EnvOr returns the value of key, or fallback if it is unset/empty.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// EnvDuration parses key as a Go duration, returning fallback if it is
// unset/empty or fails to parse.
func EnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

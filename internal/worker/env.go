package worker

import (
	"fmt"
	"os"
	"time"
)

// MustEnv returns the value of key or an error if it is unset/empty.
// SEM@fb53ed13960634e8e70bfe57a75d2305f28c4174: fetch a required environment variable or return an error if absent (pure)
func MustEnv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("worker: required env var %s is not set", key)
	}
	return v, nil
}

// EnvOr returns the value of key, or fallback if it is unset/empty.
// SEM@fb53ed13960634e8e70bfe57a75d2305f28c4174: fetch an environment variable with a fallback default (pure)
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// EnvDuration parses key as a Go duration, returning fallback if it is
// unset/empty or fails to parse.
// SEM@fb53ed13960634e8e70bfe57a75d2305f28c4174: parse an environment variable as a duration, returning a fallback on missing or invalid input (pure)
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

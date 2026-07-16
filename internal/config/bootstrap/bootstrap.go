// Package bootstrap provides the minimal, environment-only configuration a
// TMI worker component needs to start and reach the point where it can accept
// a job. It is deliberately separate from internal/config: a worker MUST NOT
// import the monolith's configuration cascade. Everything else a worker needs
// arrives in the job envelope (config.StampedConfig) or is resolved from a
// mounted secret.
package bootstrap

import (
	"fmt"
	"os"
	"strings"
)

// WorkerBootstrap is the complete startup configuration of a worker.
// SEM@7b63ecf38ff4d7eac724b5c8b36efc784b466c6f: configuration struct holding worker startup settings from environment (pure)
type WorkerBootstrap struct {
	// NATSURL is the JetStream connection URL. Required — a worker cannot
	// receive a job without it.
	NATSURL string
	// HeartbeatSubject is the NATS subject the worker publishes liveness on.
	HeartbeatSubject string
	// SecretMounts maps a logical secret name to the filesystem path of a
	// mounted Kubernetes Secret. The worker reads secret values from these
	// paths; secret values never travel over NATS or the config cascade.
	SecretMounts map[string]string
	// LogLevel is the worker log level; defaults to "info".
	LogLevel string
}

// secretMountEnvPrefix is the env-var prefix for a mounted-secret path. The
// suffix after the prefix is lowercased and each underscore becomes a dash to
// form the logical name: TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY ->
// SecretMounts["embedding-api-key"] (so a literal __ in the name becomes --).
const secretMountEnvPrefix = "TMI_WORKER_SECRET_MOUNT_" // #nosec G101 -- env-var prefix, not a credential

// LoadWorker builds a WorkerBootstrap from environment variables only.
// It reads no YAML and touches no database.
// SEM@7b63ecf38ff4d7eac724b5c8b36efc784b466c6f: build worker startup config from environment variables only, no YAML or DB (pure)
func LoadWorker() (*WorkerBootstrap, error) {
	natsURL := os.Getenv("TMI_WORKER_NATS_URL")
	if natsURL == "" {
		return nil, fmt.Errorf("worker bootstrap: required env var TMI_WORKER_NATS_URL is not set")
	}

	logLevel := os.Getenv("TMI_WORKER_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	wb := &WorkerBootstrap{
		NATSURL:          natsURL,
		HeartbeatSubject: os.Getenv("TMI_WORKER_HEARTBEAT_SUBJECT"),
		LogLevel:         logLevel,
		SecretMounts:     map[string]string{},
	}

	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		name, value := kv[:eq], kv[eq+1:]
		// Skip non-mount vars and mount vars with an empty path.
		if !strings.HasPrefix(name, secretMountEnvPrefix) || value == "" {
			continue
		}
		logical := strings.ToLower(strings.TrimPrefix(name, secretMountEnvPrefix))
		logical = strings.ReplaceAll(logical, "_", "-")
		wb.SecretMounts[logical] = value
	}

	return wb, nil
}

// ReadSecret reads the secret value for a logical name from its mounted path.
// SEM@7b63ecf38ff4d7eac724b5c8b36efc784b466c6f: fetch a mounted Kubernetes secret value by logical name from the filesystem
func (wb *WorkerBootstrap) ReadSecret(logicalName string) (string, error) {
	path, ok := wb.SecretMounts[logicalName]
	if !ok {
		return "", fmt.Errorf("worker bootstrap: no secret mount for %q", logicalName)
	}
	b, err := os.ReadFile(path) // #nosec G304 -- path comes from operator-controlled env
	if err != nil {
		return "", fmt.Errorf("worker bootstrap: read secret %q: %w", logicalName, err)
	}
	return strings.TrimSpace(string(b)), nil
}

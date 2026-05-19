package bootstrap

import (
	"os"
	"strings"
	"testing"
)

func TestLoadWorker_Success(t *testing.T) {
	t.Setenv("TMI_WORKER_NATS_URL", "nats://localhost:4222")
	t.Setenv("TMI_WORKER_HEARTBEAT_SUBJECT", "workers.heartbeat.probe")
	t.Setenv("TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY", "/var/run/secrets/embedding/key")
	t.Setenv("TMI_WORKER_LOG_LEVEL", "debug")

	wb, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if wb.NATSURL != "nats://localhost:4222" {
		t.Errorf("NATSURL = %q", wb.NATSURL)
	}
	if wb.HeartbeatSubject != "workers.heartbeat.probe" {
		t.Errorf("HeartbeatSubject = %q", wb.HeartbeatSubject)
	}
	if wb.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", wb.LogLevel)
	}
	if got := wb.SecretMounts["embedding-api-key"]; got != "/var/run/secrets/embedding/key" {
		t.Errorf("SecretMounts[embedding-api-key] = %q", got)
	}
}

func TestLoadWorker_MissingNATSURLFails(t *testing.T) {
	t.Setenv("TMI_WORKER_NATS_URL", "")
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "TMI_WORKER_NATS_URL") {
		t.Fatalf("want missing-NATS-URL error, got %v", err)
	}
}

func TestLoadWorker_LogLevelDefaults(t *testing.T) {
	t.Setenv("TMI_WORKER_NATS_URL", "nats://localhost:4222")
	t.Setenv("TMI_WORKER_HEARTBEAT_SUBJECT", "workers.heartbeat.probe")
	t.Setenv("TMI_WORKER_LOG_LEVEL", "")
	wb, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if wb.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want %q", wb.LogLevel, "info")
	}
}

func TestReadSecret(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/key"
	if err := os.WriteFile(p, []byte("  s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wb := &WorkerBootstrap{SecretMounts: map[string]string{"embedding-api-key": p}}
	got, err := wb.ReadSecret("embedding-api-key")
	if err != nil {
		t.Fatalf("ReadSecret: %v", err)
	}
	if got != "s3cr3t" {
		t.Errorf("ReadSecret = %q, want %q", got, "s3cr3t")
	}
	if _, err := wb.ReadSecret("missing"); err == nil {
		t.Error("ReadSecret for an unmounted name should error")
	}
}

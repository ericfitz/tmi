package worker

import (
	"crypto/tls"
	"testing"

	"github.com/nats-io/nats.go"
)

func applyNATSOptions(t *testing.T, opts []nats.Option) nats.Options {
	t.Helper()
	o := nats.GetDefaultOptions()
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

func TestNATSOptions_PlaintextByDefault(t *testing.T) {
	o := applyNATSOptions(t, natsOptions(Config{ComponentName: "probe"}, "", nil))
	if o.Secure || o.TLSConfig != nil {
		t.Fatal("no TLS config given: Secure/TLSConfig must stay unset (plaintext default)")
	}
	if o.Name != "tmi-probe" || o.MaxReconnect != -1 {
		t.Fatalf("name/reconnect options lost: name=%q maxReconnect=%d", o.Name, o.MaxReconnect)
	}
}

func TestNATSOptions_TLS(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "nats"}
	o := applyNATSOptions(t, natsOptions(Config{ComponentName: "probe"}, "", cfg))
	if !o.Secure || o.TLSConfig != cfg {
		t.Fatal("TLS config must be applied through nats.Secure")
	}
}

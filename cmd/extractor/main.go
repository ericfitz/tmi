// Command tmi-extractor is the sandboxed document-parse worker of the TMI
// Component Platform (issue #347). It consumes jobs.extract.* from NATS
// JetStream, parses each payload with pkg/extract, and publishes the next
// pipeline stage or a typed failure result. It runs egress: none — its only
// network peer is NATS.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/extract"
)

func main() {
	if err := run(); err != nil {
		slogging.Get().Error("tmi-extractor: %v", err)
		os.Exit(1)
	}
}

// run is the real entry point. Separating it from main allows defers to
// execute before os.Exit is called by main.
func run() error {
	logger := slogging.Get()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := worker.ConfigFromEnv()
	if err != nil {
		return fmt.Errorf("tmi-extractor: config error: %w", err)
	}

	conn, err := worker.Connect(ctx, cfg)
	if err != nil {
		return fmt.Errorf("tmi-extractor: NATS connect failed: %w", err)
	}
	defer conn.Close()

	limits := limitsFromEnv()
	handler := newExtractHandler(conn, limits)
	instanceID := worker.EnvOr("HOSTNAME", "tmi-extractor-local")

	go worker.RunHeartbeat(ctx, conn, instanceID,
		worker.EnvDuration("TMI_HEARTBEAT_INTERVAL", 0))

	logger.Info("tmi-extractor: starting consumer, component=%s", cfg.ComponentName)
	if err = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
		StreamName: worker.StreamNameFor(cfg.ComponentName),
		// Durable MUST equal the consumer name the controller pre-creates and
		// the KEDA ScaledObject watches (ConsumerNameFor) — otherwise KEDA
		// cannot observe queue depth and never scales this worker from zero.
		Durable:       worker.ConsumerNameFor(cfg.ComponentName),
		FilterSubject: worker.SubjectExtractPrefix + ">",
		AckWait:       worker.EnvDuration("TMI_JOB_ACK_WAIT", 90*time.Second),
		MaxDeliver:    3,
	}, handler.Handle); err != nil {
		return fmt.Errorf("tmi-extractor: consumer error: %w", err)
	}
	logger.Info("tmi-extractor: stopped cleanly")
	return nil
}

// limitsFromEnv builds extraction limits, overriding the design-spec
// defaults with the TMI_CONTENT_EXTRACTORS_* env vars the CR's spec.config
// supplies. Only the wall-clock budget is wired here — it is the cap a CR
// commonly tunes; the cgroup CPU/RAM caps come from the CR resources field,
// not env vars.
func limitsFromEnv() extract.Limits {
	l := extract.DefaultLimits()
	if v := worker.EnvDuration("TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET", 0); v > 0 {
		l.WallClockBudget = v
	}
	return l
}

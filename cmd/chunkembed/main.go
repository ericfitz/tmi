// Command tmi-chunk-embed is the chunk-and-embed worker of the TMI Component
// Platform (issue #347). It consumes jobs.chunkembed.* from NATS JetStream,
// splits each extracted-text payload into overlapping character windows, calls
// an OpenAI-compatible embedding API to produce dense vectors, and publishes
// the final result envelope. It runs egress: allowlist — its only network
// peers are NATS and the configured embedding API host.
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
)

func main() {
	if err := run(); err != nil {
		slogging.Get().Error("tmi-chunk-embed: %v", err)
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
		return fmt.Errorf("tmi-chunk-embed: config error: %w", err)
	}

	embCfg, err := embedConfigFromEnv()
	if err != nil {
		return fmt.Errorf("tmi-chunk-embed: embedding config error: %w", err)
	}

	embedder, err := newEmbedder(embCfg)
	if err != nil {
		return fmt.Errorf("tmi-chunk-embed: embedder build failed: %w", err)
	}

	conn, err := worker.Connect(ctx, cfg)
	if err != nil {
		return fmt.Errorf("tmi-chunk-embed: NATS connect failed: %w", err)
	}
	defer conn.Close()

	handler := newChunkEmbedHandler(conn, embedder)
	instanceID := worker.EnvOr("HOSTNAME", "tmi-chunk-embed-local")

	go worker.RunHeartbeat(ctx, conn, instanceID,
		worker.EnvDuration("TMI_HEARTBEAT_INTERVAL", 0))

	logger.Info("tmi-chunk-embed: starting consumer, component=%s", cfg.ComponentName)
	if err = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
		StreamName: worker.StreamNameFor(cfg.ComponentName),
		// Durable MUST equal the consumer name the controller pre-creates and
		// the KEDA ScaledObject watches (ConsumerNameFor) — otherwise KEDA
		// cannot observe queue depth and never scales this worker from zero.
		Durable:       worker.ConsumerNameFor(cfg.ComponentName),
		FilterSubject: worker.SubjectChunkEmbedPrefix + ">",
		AckWait:       worker.EnvDuration("TMI_JOB_ACK_WAIT", 120*time.Second),
		MaxDeliver:    3,
	}, handler.Handle); err != nil {
		return fmt.Errorf("tmi-chunk-embed: consumer error: %w", err)
	}
	logger.Info("tmi-chunk-embed: stopped cleanly")
	return nil
}

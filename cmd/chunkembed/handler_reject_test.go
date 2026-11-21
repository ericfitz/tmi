package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/internal/llm"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/extract"
)

type fakeJobConn struct {
	getCalls              int
	failJobID, failReason string
	failDetail            string
}

func (f *fakeJobConn) GetPayload(context.Context, string) ([]byte, error) {
	f.getCalls++
	return []byte("hello"), nil
}
func (f *fakeJobConn) PutPayload(context.Context, string, []byte) (string, error) { return "ref", nil }
func (f *fakeJobConn) Publish(context.Context, string, []byte) error              { return nil }
func (f *fakeJobConn) PublishFailureResult(_ context.Context, jobID, reason, detail string) error {
	f.failJobID, f.failReason, f.failDetail = jobID, reason, detail
	return &worker.JobError{ReasonCode: reason, Detail: detail, Terminal: true}
}

func TestHandleRejectedEndpointPublishesFailure(t *testing.T) {
	pool, built := newTestPool(t, "")
	conn := &fakeJobConn{}
	h := &chunkEmbedHandler{conn: conn, chunker: extract.NewTextChunker(512, 50), embedders: pool}

	job := jobWithProfile("m", "https://evil.example.net/v1?key=sekrit", 8)
	job.Input.ObjectRef = "b/k"
	err := h.Handle(context.Background(), job)

	var je *worker.JobError
	if !errors.As(err, &je) || !je.Terminal {
		t.Fatalf("expected terminal JobError, got %v", err)
	}
	if conn.failReason != "embedding_endpoint_not_allowed" || conn.failJobID != "j1" {
		t.Fatalf("failure not published with expected reason: %+v", conn)
	}
	if conn.getCalls != 0 || len(*built) != 0 {
		t.Fatalf("rejected job must not read payload or build an embedder")
	}
	if containsStr(conn.failDetail, "sekrit") || !containsStr(conn.failDetail, "evil.example.net") {
		t.Fatalf("detail must name host and no secrets: %q", conn.failDetail)
	}
}

var _ llm.Embedder = (*stubEmbedder)(nil)

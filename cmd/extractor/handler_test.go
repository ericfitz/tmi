package main

import (
	"strings"
	"testing"

	"github.com/ericfitz/tmi/pkg/extract"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

func TestSubjectTypeToken(t *testing.T) {
	cases := map[string]string{
		"application/pdf": "pdf",
		"text/html":       "html",
		"text/plain":      "plaintext",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "ooxml",
		"application/octet-stream": "plaintext", // unknown defaults to plaintext
	}
	for ct, want := range cases {
		if got := subjectTypeToken(ct); got != want {
			t.Fatalf("subjectTypeToken(%q): got %q want %q", ct, got, want)
		}
	}
}

func TestExtractDispatch(t *testing.T) {
	reg := buildExtractorRegistry(extract.DefaultLimits())
	ext, ok := reg.FindExtractor("text/plain")
	if !ok {
		t.Fatal("no extractor for text/plain")
	}
	out, err := ext.Extract([]byte("hello world"), "text/plain")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(out.Text, "hello world") {
		t.Fatalf("plaintext extract: got %q", out.Text)
	}
}

func TestBuildNextJobForwardsConfig(t *testing.T) {
	cfg := &jobenvelope.Config{Embedding: &jobenvelope.EmbeddingProfile{
		Model: "m", Endpoint: "https://h/v1", Dimension: 8,
	}}
	in := jobenvelope.Job{JobID: "j1", ContentType: "text/plain", Config: cfg,
		Input: jobenvelope.Input{ObjectRef: "src"}}
	next := buildNextJob(in, "ref/extracted", extract.ExtractedContent{Text: "hello"})
	if next.Config != cfg {
		t.Fatalf("config not forwarded unchanged: %+v", next.Config)
	}
	if next.Input.ObjectRef != "ref/extracted" || next.Input.ByteSize != 5 || next.JobID != "j1" {
		t.Fatalf("unexpected next job: %+v", next)
	}
	// An old envelope without config stays without config.
	in.Config = nil
	if buildNextJob(in, "r", extract.ExtractedContent{}).Config != nil {
		t.Fatal("nil config must stay nil")
	}
}

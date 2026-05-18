package main

import (
	"strings"
	"testing"

	"github.com/ericfitz/tmi/pkg/extract"
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

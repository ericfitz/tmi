package config

import (
	"encoding/json"
	"testing"
)

func TestStampedConfig_JSONRoundTrip(t *testing.T) {
	in := StampedConfig{
		Embedding: EmbeddingProfile{
			Model:     "text-embedding-3-large",
			Endpoint:  "https://api.openai.com/v1",
			Dimension: 3072,
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out StampedConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Embedding != in.Embedding {
		t.Errorf("round trip mismatch: got %+v, want %+v", out.Embedding, in.Embedding)
	}
}

func TestEmbeddingProfile_Valid(t *testing.T) {
	good := EmbeddingProfile{Model: "m", Endpoint: "https://e", Dimension: 768}
	if err := good.Validate(); err != nil {
		t.Errorf("valid profile rejected: %v", err)
	}
	bad := EmbeddingProfile{Model: "", Endpoint: "https://e", Dimension: 768}
	if err := bad.Validate(); err == nil {
		t.Error("profile with empty model should be invalid")
	}
	badDim := EmbeddingProfile{Model: "m", Endpoint: "https://e", Dimension: 0}
	if err := badDim.Validate(); err == nil {
		t.Error("profile with zero dimension should be invalid")
	}
}

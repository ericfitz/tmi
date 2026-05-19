package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
)

// fakeStringGetter implements the minimal settings-read surface the provider needs.
type fakeStringGetter struct {
	vals map[string]string
}

func (f fakeStringGetter) GetString(ctx context.Context, key string) (string, error) {
	return f.vals[key], nil
}
func (f fakeStringGetter) GetInt(ctx context.Context, key string) (int, error) {
	switch f.vals[key] {
	case "768":
		return 768, nil
	case "3072":
		return 3072, nil
	}
	return 0, nil
}

func TestStampedConfigProvider_Get(t *testing.T) {
	g := fakeStringGetter{vals: map[string]string{
		"timmy.text_embedding_model":    "text-embedding-3-large",
		"timmy.text_embedding_base_url": "https://api.openai.com/v1",
		"timmy.embedding_dimension":     "3072",
	}}
	p := NewStampedConfigProvider(g)
	sc, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := config.EmbeddingProfile{
		Model:     "text-embedding-3-large",
		Endpoint:  "https://api.openai.com/v1",
		Dimension: 3072,
	}
	if sc.Embedding != want {
		t.Errorf("Get() embedding = %+v, want %+v", sc.Embedding, want)
	}
}

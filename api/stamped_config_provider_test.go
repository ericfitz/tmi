package api

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
)

// fakeSettingsReader implements the minimal settings-read surface the provider needs.
type fakeSettingsReader struct {
	vals map[string]string
}

func (f fakeSettingsReader) GetString(_ context.Context, key string) (string, error) {
	return f.vals[key], nil
}
func (f fakeSettingsReader) GetInt(_ context.Context, key string) (int, error) {
	v := f.vals[key]
	if v == "" {
		return 0, nil
	}
	return strconv.Atoi(v)
}

func TestStampedConfigProvider_Get(t *testing.T) {
	g := fakeSettingsReader{vals: map[string]string{
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

// erroringGetter fails on a specified key to exercise Get's error paths.
type erroringGetter struct {
	failKey string
}

func (e erroringGetter) GetString(_ context.Context, key string) (string, error) {
	if key == e.failKey {
		return "", fmt.Errorf("boom: %s", key)
	}
	return "ok", nil
}
func (e erroringGetter) GetInt(_ context.Context, key string) (int, error) {
	if key == e.failKey {
		return 0, fmt.Errorf("boom: %s", key)
	}
	return 1, nil
}

func TestStampedConfigProvider_Get_PropagatesErrors(t *testing.T) {
	for _, failKey := range []string{
		"timmy.text_embedding_model",
		"timmy.text_embedding_base_url",
		"timmy.embedding_dimension",
	} {
		p := NewStampedConfigProvider(erroringGetter{failKey: failKey})
		_, err := p.Get(context.Background())
		if err == nil {
			t.Errorf("Get with failing key %q: want error, got nil", failKey)
		}
	}
}

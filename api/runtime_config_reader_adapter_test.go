package api

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/api/models"
)

// fakeSettingsService is a minimal SettingsServiceInterface stub for adapter
// tests. Only the methods the adapter actually calls are exercised.
type fakeSettingsService struct {
	strings map[string]string
	errs    map[string]error
}

func newFakeSettingsService() *fakeSettingsService {
	return &fakeSettingsService{
		strings: map[string]string{},
		errs:    map[string]error{},
	}
}

func (f *fakeSettingsService) Get(ctx context.Context, key string) (*models.SystemSetting, error) {
	return nil, nil
}
func (f *fakeSettingsService) GetString(ctx context.Context, key string) (string, error) {
	if err, ok := f.errs[key]; ok {
		return "", err
	}
	return f.strings[key], nil
}
func (f *fakeSettingsService) GetInt(ctx context.Context, key string) (int, error) { return 0, nil }
func (f *fakeSettingsService) GetBool(ctx context.Context, key string) (bool, error) {
	return false, nil
}
func (f *fakeSettingsService) List(ctx context.Context) ([]models.SystemSetting, error) {
	return nil, nil
}
func (f *fakeSettingsService) ListByPrefix(ctx context.Context, prefix string) ([]models.SystemSetting, error) {
	return nil, nil
}
func (f *fakeSettingsService) Set(ctx context.Context, setting *models.SystemSetting) error {
	return nil
}
func (f *fakeSettingsService) Delete(ctx context.Context, key string) error { return nil }
func (f *fakeSettingsService) SeedDefaults(ctx context.Context) error       { return nil }
func (f *fakeSettingsService) ReEncryptAll(ctx context.Context, modifiedBy *string) (int, []SettingError, error) {
	return 0, nil, nil
}

func TestRuntimeConfigReaderAdapter_GetClientCallbackAllowList(t *testing.T) {
	ctx := context.Background()

	t.Run("valid JSON returns the slice with exists=true", func(t *testing.T) {
		f := newFakeSettingsService()
		f.strings["auth.oauth.client_callback_allowlist"] = `["http://a/","http://b/*"]`
		a := NewRuntimeConfigReaderAdapter(f)
		list, exists, err := a.GetClientCallbackAllowList(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists {
			t.Error("exists = false, want true")
		}
		if len(list) != 2 || list[0] != "http://a/" || list[1] != "http://b/*" {
			t.Errorf("got %#v, want [http://a/ http://b/*]", list)
		}
	})

	t.Run("missing row: exists=false, no error", func(t *testing.T) {
		a := NewRuntimeConfigReaderAdapter(newFakeSettingsService())
		list, exists, err := a.GetClientCallbackAllowList(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists {
			t.Error("exists = true, want false for missing row")
		}
		if list != nil {
			t.Errorf("got %#v, want nil", list)
		}
	})

	t.Run("read error: exists=true, error returned (fail-closed)", func(t *testing.T) {
		f := newFakeSettingsService()
		f.errs["auth.oauth.client_callback_allowlist"] = errors.New("boom")
		a := NewRuntimeConfigReaderAdapter(f)
		_, exists, err := a.GetClientCallbackAllowList(ctx)
		if err == nil {
			t.Error("want error, got nil")
		}
		if !exists {
			t.Error("read error should report exists=true so the caller fails closed")
		}
	})

	t.Run("malformed JSON: exists=true, error returned (fail-closed)", func(t *testing.T) {
		f := newFakeSettingsService()
		f.strings["auth.oauth.client_callback_allowlist"] = `not json`
		a := NewRuntimeConfigReaderAdapter(f)
		_, exists, err := a.GetClientCallbackAllowList(ctx)
		if err == nil {
			t.Error("want error, got nil")
		}
		if !exists {
			t.Error("malformed JSON should report exists=true so the caller fails closed")
		}
	})
}

func TestRuntimeConfigReaderAdapter_IsSAMLEnabled(t *testing.T) {
	ctx := context.Background()

	t.Run("true", func(t *testing.T) {
		f := newFakeSettingsService()
		f.strings["features.saml_enabled"] = "true"
		a := NewRuntimeConfigReaderAdapter(f)
		if !a.IsSAMLEnabled(ctx) {
			t.Error("want true, got false")
		}
	})

	t.Run("false", func(t *testing.T) {
		f := newFakeSettingsService()
		f.strings["features.saml_enabled"] = "false"
		a := NewRuntimeConfigReaderAdapter(f)
		if a.IsSAMLEnabled(ctx) {
			t.Error("want false, got true")
		}
	})

	t.Run("missing row defaults false (fail-closed)", func(t *testing.T) {
		a := NewRuntimeConfigReaderAdapter(newFakeSettingsService())
		if a.IsSAMLEnabled(ctx) {
			t.Error("want false on missing row, got true")
		}
	})

	t.Run("garbage value defaults false", func(t *testing.T) {
		f := newFakeSettingsService()
		f.strings["features.saml_enabled"] = "yes-please"
		a := NewRuntimeConfigReaderAdapter(f)
		if a.IsSAMLEnabled(ctx) {
			t.Error("want false on garbage value, got true")
		}
	})
}

func TestRuntimeConfigReaderAdapter_GetOAuthCallbackURL(t *testing.T) {
	ctx := context.Background()

	t.Run("returns configured value", func(t *testing.T) {
		f := newFakeSettingsService()
		f.strings["auth.oauth_callback_url"] = "http://x/oauth/callback"
		a := NewRuntimeConfigReaderAdapter(f)
		if got := a.GetOAuthCallbackURL(ctx); got != "http://x/oauth/callback" {
			t.Errorf("got %q, want http://x/oauth/callback", got)
		}
	})

	t.Run("missing row returns empty (caller falls back to YAML)", func(t *testing.T) {
		a := NewRuntimeConfigReaderAdapter(newFakeSettingsService())
		if got := a.GetOAuthCallbackURL(ctx); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

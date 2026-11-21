package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
)

// fakeSettingsService is a minimal SettingsServiceInterface stub for adapter
// tests. Only the methods the adapter actually calls are exercised.
// db simulates resolved rows (GetResolvedString); strings simulates the
// config layer (GetString reads ONLY this map; the maps are deliberately
// disjoint so a test can tell which layer answered). errs fails both reads for a key; dbErrs
// fails only the DB read (wrap dberrors.ErrTransient to simulate an ADB blip).
type fakeSettingsService struct {
	db      map[string]string
	strings map[string]string
	errs    map[string]error
	dbErrs  map[string]error
}

func newFakeSettingsService() *fakeSettingsService {
	return &fakeSettingsService{
		db:      map[string]string{},
		strings: map[string]string{},
		errs:    map[string]error{},
		dbErrs:  map[string]error{},
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

func (f *fakeSettingsService) GetResolvedString(ctx context.Context, key string) (string, bool, error) {
	if err, ok := f.dbErrs[key]; ok {
		return "", false, err
	}
	if err, ok := f.errs[key]; ok {
		return "", false, err
	}
	v, ok := f.db[key]
	return v, ok, nil
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

func (f *fakeSettingsService) SeedDefaults(ctx context.Context) error { return nil }

func (f *fakeSettingsService) ReEncryptAll(ctx context.Context) (int, []SettingError, error) {
	return 0, nil, nil
}

func (f *fakeSettingsService) CountValuesWithContextID(ctx context.Context, id int) (int64, error) {
	return 0, nil
}

func TestRuntimeConfigReaderAdapter_GetClientCallbackAllowList(t *testing.T) {
	ctx := context.Background()

	t.Run("valid JSON returns the slice with exists=true", func(t *testing.T) {
		f := newFakeSettingsService()
		f.db["auth.oauth.client_callback_allowlist"] = `["http://a/","http://b/*"]`
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
		f.db["auth.oauth.client_callback_allowlist"] = `not json`
		a := NewRuntimeConfigReaderAdapter(f)
		_, exists, err := a.GetClientCallbackAllowList(ctx)
		if err == nil {
			t.Error("want error, got nil")
		}
		if !exists {
			t.Error("malformed JSON should report exists=true so the caller fails closed")
		}
	})

	// Regression for #767: a YAML/env value for the same key must NOT shadow
	// the DB row — the DB wins at request time; YAML is a first-run fallback.
	t.Run("DB row wins over config value (#767)", func(t *testing.T) {
		f := newFakeSettingsService()
		f.strings["auth.oauth.client_callback_allowlist"] = `["http://yaml-only/"]`
		f.db["auth.oauth.client_callback_allowlist"] = `["http://from-db/*"]`
		a := NewRuntimeConfigReaderAdapter(f)
		list, exists, err := a.GetClientCallbackAllowList(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists {
			t.Fatal("exists = false, want true")
		}
		if len(list) != 1 || list[0] != "http://from-db/*" {
			t.Errorf("got %#v, want the DB value [http://from-db/*]", list)
		}
	})

	t.Run("config value alone does not count as a DB row", func(t *testing.T) {
		f := newFakeSettingsService()
		f.strings["auth.oauth.client_callback_allowlist"] = `["http://yaml-only/"]`
		a := NewRuntimeConfigReaderAdapter(f)
		_, exists, err := a.GetClientCallbackAllowList(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists {
			t.Error("exists = true, want false — caller must fall back to its own YAML snapshot")
		}
	})
}

// Transient-DB-fault fallback (#767 oracle-db-admin review, blocking item):
// an ADB blip (ORA-02396/03113/12537 class) must NOT fail closed — the
// operator's YAML snapshot answers instead. Only non-transient errors keep
// the fail-closed contract.
func TestRuntimeConfigReaderAdapter_TransientDBFaultFallsBackToYAML(t *testing.T) {
	ctx := context.Background()
	transient := fmt.Errorf("simulated ADB blip: %w", dberrors.ErrTransient)

	t.Run("allowlist: transient error reads as no-row so the caller uses YAML", func(t *testing.T) {
		f := newFakeSettingsService()
		f.dbErrs["auth.oauth.client_callback_allowlist"] = transient
		a := NewRuntimeConfigReaderAdapter(f)
		list, exists, err := a.GetClientCallbackAllowList(ctx)
		if err != nil {
			t.Fatalf("transient fault must not surface an error (fail-closed), got: %v", err)
		}
		if exists {
			t.Error("exists = true, want false — caller must fall back to the YAML snapshot")
		}
		if list != nil {
			t.Errorf("got %#v, want nil", list)
		}
	})

	t.Run("allowlist: non-transient error still fails closed", func(t *testing.T) {
		f := newFakeSettingsService()
		f.dbErrs["auth.oauth.client_callback_allowlist"] = errors.New("decrypt failure")
		a := NewRuntimeConfigReaderAdapter(f)
		_, exists, err := a.GetClientCallbackAllowList(ctx)
		if err == nil {
			t.Error("want error for a non-transient fault")
		}
		if !exists {
			t.Error("non-transient fault must keep exists=true so the caller fails closed")
		}
	})
}

// IsEveryoneAReviewer is the fifth key #794 catalogued. Before this change it
// had no runtime reader at all — the only consumer read the config struct
// directly, so the database row was visible, editable, and inert.
func TestRuntimeConfigReaderAdapter_IsEveryoneAReviewer(t *testing.T) {
	const key = "auth.everyone_is_a_reviewer"

	tests := []struct {
		name  string
		setup func(f *fakeSettingsService)
		want  bool
	}{
		{
			name:  "resolved true",
			setup: func(f *fakeSettingsService) { f.db[key] = "true" },
			want:  true,
		},
		{
			name:  "resolved false",
			setup: func(f *fakeSettingsService) { f.db[key] = "false" },
			want:  false,
		},
		{
			name:  "no value anywhere is fail-closed",
			setup: func(f *fakeSettingsService) {},
			want:  false,
		},
		{
			name:  "unparseable value is fail-closed",
			setup: func(f *fakeSettingsService) { f.db[key] = "yes-please" },
			want:  false,
		},
		{
			name:  "read error is fail-closed",
			setup: func(f *fakeSettingsService) { f.errs[key] = errors.New("boom") },
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeSettingsService()
			tt.setup(f)
			a := NewRuntimeConfigReaderAdapter(f)

			if got := a.IsEveryoneAReviewer(context.Background()); got != tt.want {
				t.Errorf("IsEveryoneAReviewer() = %v, want %v", got, tt.want)
			}
		})
	}
}

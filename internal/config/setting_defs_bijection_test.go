package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// envTaggedKeys walks the Config struct and returns every yaml path that
// carries an env tag, as "a.b.c" joined from the yaml tags along the path,
// mapped to that field's env tag name. Reflection is used here deliberately
// and only in tests.
func envTaggedKeys(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}

	var walk func(rt reflect.Type, prefix []string)
	walk = func(rt reflect.Type, prefix []string) {
		if rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			yamlTag := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if yamlTag == "-" {
				continue
			}
			path := prefix
			if yamlTag != "" {
				path = append(append([]string{}, prefix...), yamlTag)
			}
			if env := f.Tag.Get("env"); env != "" {
				out[strings.Join(path, ".")] = env
			}
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				walk(ft, path)
			}
		}
	}
	walk(reflect.TypeOf(Config{}), nil)
	return out
}

// allYAMLPaths walks the Config struct and returns every yaml path that
// exists, as a set — regardless of whether that field also carries an env
// tag. This is the superset envTaggedKeys draws from: it is what lets a
// SettingDef's YAMLPath be checked for truthfulness even on a
// config-file-only field with no env binding at all (administrators,
// every ssrf.*.allowlist/schemes key).
func allYAMLPaths(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}

	var walk func(rt reflect.Type, prefix []string)
	walk = func(rt reflect.Type, prefix []string) {
		if rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			yamlTag := strings.Split(f.Tag.Get("yaml"), ",")[0]
			if yamlTag == "-" {
				continue
			}
			path := prefix
			if yamlTag != "" {
				path = append(append([]string{}, prefix...), yamlTag)
				out[strings.Join(path, ".")] = true
			}
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				walk(ft, path)
			}
		}
	}
	walk(reflect.TypeOf(Config{}), nil)
	return out
}

// envVarSet collapses envTaggedKeys down to the set of distinct env: tag
// values bound somewhere on Config, discarding which yaml path(s) they
// belong to.
func envVarSet(t *testing.T) map[string]bool {
	structKeys := envTaggedKeys(t)
	out := make(map[string]bool, len(structKeys))
	for _, env := range structKeys {
		out[env] = true
	}
	return out
}

// TestSettingDefs_BijectWithConfigStructEnvVars is the bijection test for
// the registry's EnvVar declarations. It is keyed on EnvVar rather than
// YAMLPath (a prior version of this test compared YAMLPath -> EnvVar maps,
// which had two problems: a def with a real YAMLPath but no env tag — e.g.
// administrators, ssrf.* — always registered as "extra" even though the
// yaml key genuinely exists, and TMI_JWT_EXPIRATION_SECONDS is legitimately
// claimed by two defs, auth.jwt.expiration_seconds and the derived
// session.timeout_minutes, which a map keyed by env var cannot represent
// without one silently overwriting the other). Comparing as sets tolerates
// both: YAMLPath truthfulness is checked separately by
// TestSettingDefs_YAMLPathsAreReal below, and a set has no trouble with two
// defs sharing one EnvVar.
func TestSettingDefs_BijectWithConfigStructEnvVars(t *testing.T) {
	structEnvVars := envVarSet(t)

	declared := map[string]bool{}
	for _, d := range AllSettingDefs() {
		if d.EnvVar == "" {
			continue
		}
		declared[d.EnvVar] = true
	}

	var missing, extra []string
	for k := range structEnvVars {
		if !declared[k] {
			missing = append(missing, k)
		}
	}
	for k := range declared {
		if !structEnvVars[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	assert.Empty(t, missing, "Config struct env: tags with no SettingDef claiming them")
	assert.Empty(t, extra, "SettingDefs claiming an EnvVar the Config struct does not bind")
}

// TestBootstrapDefs_EnvVarNamesMatchStructTags checks, for every def that
// declares a YAMLPath the struct walk also recognizes as env-tagged, that
// the def's EnvVar agrees with the struct tag. A def whose YAMLPath has no
// env tag (administrators, ssrf.*) is skipped here — reported instead by
// TestSettingDefs_YAMLPathsAreReal — and a def with no YAMLPath at all
// (the derived session.timeout_minutes) is skipped unconditionally.
func TestBootstrapDefs_EnvVarNamesMatchStructTags(t *testing.T) {
	structKeys := envTaggedKeys(t)
	for _, d := range AllSettingDefs() {
		if d.YAMLPath == "" {
			continue
		}
		want, ok := structKeys[d.YAMLPath]
		if !ok {
			continue // reported by the bijection test
		}
		assert.Equal(t, want, d.EnvVar,
			"SettingDef %q declares EnvVar %q but the Config struct binds %q",
			d.Key, d.EnvVar, want)
	}
}

// TestSettingDefs_YAMLPathsAreReal asserts every non-empty YAMLPath declared
// in the registry names an actual yaml-tagged Config struct field —
// including a field with no env: tag, like administrators and every
// ssrf.*.allowlist/schemes key. This closes the blind spot the EnvVar-keyed
// bijection test above cannot see: without this test, a def could declare
// any string (or an empty one) as its YAMLPath and nothing would catch the
// difference between "the path is real" and "the author wrote it once and
// moved on" — which is exactly what let administrators and the ssrf.* keys
// carry a false empty YAMLPath in an earlier version of this registry.
func TestSettingDefs_YAMLPathsAreReal(t *testing.T) {
	realPaths := allYAMLPaths(t)
	for _, d := range AllSettingDefs() {
		if d.YAMLPath == "" {
			continue
		}
		assert.True(t, realPaths[d.YAMLPath],
			"SettingDef %q declares YAMLPath %q, which is not a yaml-tagged field on Config", d.Key, d.YAMLPath)
	}
}

func TestBootstrapDefs_GetReturnsConfiguredValue(t *testing.T) {
	c := &Config{}
	c.Server.Port = "9999"

	d, ok := DefFor("server.port")
	assert.True(t, ok, "server.port must be declared")
	if ok {
		assert.Equal(t, "9999", d.Get(c))
	}
}

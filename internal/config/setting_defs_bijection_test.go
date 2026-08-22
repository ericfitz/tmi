package config

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// envTaggedKeys walks the Config struct and returns every yaml path that
// carries an env tag, as "a.b.c" joined from the yaml tags along the path.
// Reflection is used here deliberately and only in tests.
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

func TestBootstrapDefs_BijectWithConfigStructEnvTags(t *testing.T) {
	structKeys := envTaggedKeys(t)

	declared := map[string]string{}
	for _, d := range AllSettingDefs() {
		if d.YAMLPath == "" {
			continue // no config path: database-only operational setting
		}
		declared[d.YAMLPath] = d.EnvVar
	}

	var missing, extra []string
	for k := range structKeys {
		if _, ok := declared[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range declared {
		if _, ok := structKeys[k]; !ok {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	assert.Empty(t, missing, "Config struct fields with an env tag that have no SettingDef")
	assert.Empty(t, extra, "SettingDefs claiming a config path that the Config struct does not bind")
}

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

func TestBootstrapDefs_GetReturnsConfiguredValue(t *testing.T) {
	c := &Config{}
	c.Server.Port = "9999"

	d, ok := DefFor("server.port")
	assert.True(t, ok, "server.port must be declared")
	if ok {
		assert.Equal(t, "9999", d.Get(c))
	}
}

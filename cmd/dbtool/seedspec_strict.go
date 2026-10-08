package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// rawMessageType is opaque to the unknown-field walk: survey_json, settings
// and responses are free-form by design.
var rawMessageType = reflect.TypeFor[json.RawMessage]()

// unknownSpecFields lists every JSON key in a seed spec that no SeedSpec*
// struct field declares, as dotted paths (array indexes in brackets).
//
// encoding/json drops such keys silently, so a spec field the transform does
// not know about (a team's responsible_parties, say) would seed an object that
// differs from the spec without any error. DisallowUnknownFields only reports
// the first offender; this walk reports all of them so a spec author can fix
// the file in one pass. Keys starting with "_" are comments and are accepted
// anywhere.
func unknownSpecFields(data []byte) ([]string, error) {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var unknown []string
	walkUnknownFields(doc, reflect.TypeFor[SeedSpecFile](), "", &unknown)
	sort.Strings(unknown)
	return unknown, nil
}

func walkUnknownFields(v any, t reflect.Type, path string, unknown *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == rawMessageType {
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok {
			return // type mismatches are reported by the real decode
		}
		fields := jsonFieldTypes(t)
		for key, child := range obj {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if strings.HasPrefix(key, "_") {
				continue // "_comment" and similar: documentation, never seeded
			}
			ft, known := fields[key]
			if !known {
				*unknown = append(*unknown, childPath)
				continue
			}
			walkUnknownFields(child, ft, childPath, unknown)
		}
	case reflect.Slice, reflect.Array:
		arr, ok := v.([]any)
		if !ok {
			return
		}
		for i, child := range arr {
			walkUnknownFields(child, t.Elem(), fmt.Sprintf("%s[%d]", path, i), unknown)
		}
	default:
		// Scalars, maps and interfaces accept any key.
	}
}

func jsonFieldTypes(t reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" || !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		fields[name] = f.Type
	}
	return fields
}

package api

import (
	"encoding/json"
	"reflect"
	"strings"
)

var jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()

// findCaseAliasedKey returns the JSON pointer of the first object key in doc
// that is not an exact JSON field name of the corresponding Go struct but
// equals one case-insensitively (e.g. "Owner" for "owner"). encoding/json
// would silently decode such a key into that field, so a PATCH to "/Owner"
// could change owner past checks that compare paths exactly. Keys under
// map-typed fields are data and are not checked.
// SEM@95fbc20511ce80f464d07e7723483035dbf87346: find a patched JSON key that case-aliases a struct field name (pure)
func findCaseAliasedKey(doc any, t reflect.Type, ptr string) (string, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(jsonUnmarshalerType) || t.Implements(jsonUnmarshalerType) {
		return "", false // custom decoding; not a plain field mapping
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := doc.(map[string]any)
		if !ok {
			return "", false
		}
		fields := jsonFieldTypes(t)
		for k, v := range obj {
			if ft, exact := fields[k]; exact {
				if p, bad := findCaseAliasedKey(v, ft, ptr+"/"+k); bad {
					return p, true
				}
				continue
			}
			for name := range fields {
				if strings.EqualFold(name, k) {
					return ptr + "/" + k, true
				}
			}
		}
	case reflect.Slice, reflect.Array:
		if arr, ok := doc.([]any); ok {
			for _, v := range arr {
				if p, bad := findCaseAliasedKey(v, t.Elem(), ptr+"/-"); bad {
					return p, true
				}
			}
		}
	case reflect.Map:
		if obj, ok := doc.(map[string]any); ok {
			for k, v := range obj {
				if p, bad := findCaseAliasedKey(v, t.Elem(), ptr+"/"+k); bad {
					return p, true
				}
			}
		}
	}
	return "", false
}

// jsonFieldTypes maps each JSON field name of struct t to its type, following
// encoding/json rules (tag name, else Go name; "-" skipped; untagged
// embedded structs flattened).
// SEM@95fbc20511ce80f464d07e7723483035dbf87346: map a struct's JSON field names to their Go types per encoding/json rules (pure)
func jsonFieldTypes(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		ft := f.Type
		if f.Anonymous && name == "" {
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for n, typ := range jsonFieldTypes(ft) {
					if _, dup := out[n]; !dup {
						out[n] = typ
					}
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = ft
	}
	return out
}

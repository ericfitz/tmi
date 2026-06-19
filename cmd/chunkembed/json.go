package main

import (
	"encoding/json"
	"fmt"
)

// jsonMarshal wraps json.Marshal with a labelled error.
// SEM@ef969bb79ad525fa5038847af0fb0be1038ae961: serialize a value to JSON bytes with a labeled error on failure (pure)
func jsonMarshal(v any, label string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", label, err)
	}
	return b, nil
}

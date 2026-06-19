package main

import (
	"encoding/json"
	"fmt"
)

// jsonMarshal wraps json.Marshal with a labelled error.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: serialize a value to JSON bytes, wrapping errors with a caller-supplied label (pure)
func jsonMarshal(v any, label string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", label, err)
	}
	return b, nil
}

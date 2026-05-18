package main

import (
	"encoding/json"
	"fmt"
)

// jsonMarshal wraps json.Marshal with a labelled error.
func jsonMarshal(v any, label string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", label, err)
	}
	return b, nil
}

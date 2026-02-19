package api

import (
	"encoding/base64"
	"fmt"
	"unicode/utf8"
)

// preprocessPatchOperations handles special cases in patch operations before applying them
func preprocessPatchOperations(operations []PatchOperation) ([]PatchOperation, error) {
	processedOps := make([]PatchOperation, len(operations))

	for i, op := range operations {
		processedOp := op

		// Handle base64 SVG decoding for /image/svg path
		if op.Path == "/image/svg" && (op.Op == string(Replace) || op.Op == string(Add)) {
			if svgString, ok := op.Value.(string); ok {
				// Decode base64 SVG string to bytes
				svgBytes, err := base64.StdEncoding.DecodeString(svgString)
				if err != nil {
					return nil, fmt.Errorf("failed to decode base64 SVG data: %w", err)
				}

				// Validate that the decoded bytes are valid UTF-8
				if !utf8.Valid(svgBytes) {
					return nil, fmt.Errorf("SVG data contains invalid UTF-8 sequences")
				}

				// Set the value to the decoded bytes
				processedOp.Value = svgBytes
			}
		}

		processedOps[i] = processedOp
	}

	return processedOps, nil
}

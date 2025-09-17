package api

import (
	"encoding/base64"
)

// preprocessPatchOperations handles special cases in patch operations before applying them
func preprocessPatchOperations(operations []PatchOperation) ([]PatchOperation, error) {
	processedOps := make([]PatchOperation, len(operations))

	for i, op := range operations {
		processedOp := op

		// Handle base64 SVG decoding for /image/svg path
		if op.Path == "/image/svg" && (op.Op == "replace" || op.Op == "add") {
			if svgString, ok := op.Value.(string); ok {
				// Decode base64 SVG string to bytes
				svgBytes, err := base64.StdEncoding.DecodeString(svgString)
				if err != nil {
					return nil, err
				}
				// Set the value to the decoded bytes
				processedOp.Value = svgBytes
			}
		}

		processedOps[i] = processedOp
	}

	return processedOps, nil
}

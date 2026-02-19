package api

import (
	"encoding/json"
	"strconv"
)

// parseInt converts a string to an integer with a fallback value
func parseInt(val string, fallback int) (int, error) {
	if val == "" {
		return fallback, nil
	}

	i, err := strconv.Atoi(val)
	if err != nil {
		return fallback, err
	}

	return i, nil
}

// applyJsonPatch applies JSON Patch operations to a value
// This is a simplified implementation
func applyJsonPatch(doc interface{}, operations []PatchOperation) (interface{}, error) {
	// Convert document to JSON
	docJson, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}

	// Parse the document as a generic JSON object
	var docMap map[string]interface{}
	err = json.Unmarshal(docJson, &docMap)
	if err != nil {
		return nil, err
	}

	// Apply each operation
	for _, op := range operations {
		switch op.Op {
		case string(Add):
			// Implementation would add value at path
		case string(Remove):
			// Implementation would remove value at path
		case string(Replace):
			// Implementation would replace value at path
		case string(Move):
			// Implementation would move value from -> path
		case string(Copy):
			// Implementation would copy value from -> path
		case string(Test):
			// Implementation would test if value at path equals op.Value
		}
	}

	// In a real implementation, you would apply the patch operations
	// For simplicity, we're just returning the original document
	return doc, nil
}

package api

import (
	"encoding/base64"
	"testing"
)

func TestPreprocessPatchOperations_UTF8Validation(t *testing.T) {
	tests := []struct {
		name        string
		operations  []PatchOperation
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid UTF-8 SVG",
			operations: []PatchOperation{
				{
					Op:    "replace",
					Path:  "/image/svg",
					Value: base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><text>Valid UTF-8</text></svg>`)),
				},
			},
			expectError: false,
		},
		{
			name: "invalid UTF-8 SVG with 0xa0 byte",
			operations: []PatchOperation{
				{
					Op:    "replace",
					Path:  "/image/svg",
					Value: base64.StdEncoding.EncodeToString([]byte{0x3C, 0x73, 0x76, 0x67, 0x3E, 0xa0, 0x3C, 0x2F, 0x73, 0x76, 0x67, 0x3E}), // <svg>�</svg> with invalid 0xa0
				},
			},
			expectError: true,
			errorMsg:    "SVG data contains invalid UTF-8 sequences",
		},
		{
			name: "invalid UTF-8 SVG with multiple invalid bytes",
			operations: []PatchOperation{
				{
					Op:    "replace",
					Path:  "/image/svg",
					Value: base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0x3C, 0x73, 0x76, 0x67, 0x3E, 0x3C, 0x2F, 0x73, 0x76, 0x67, 0x3E}), // invalid UTF-8 prefix
				},
			},
			expectError: true,
			errorMsg:    "SVG data contains invalid UTF-8 sequences",
		},
		{
			name: "invalid base64 encoding",
			operations: []PatchOperation{
				{
					Op:    "replace",
					Path:  "/image/svg",
					Value: "invalid-base64!!!",
				},
			},
			expectError: true,
			errorMsg:    "failed to decode base64 SVG data",
		},
		{
			name: "non-SVG path should not be processed",
			operations: []PatchOperation{
				{
					Op:    "replace",
					Path:  "/name",
					Value: "some value",
				},
			},
			expectError: false,
		},
		{
			name: "add operation with valid UTF-8 SVG",
			operations: []PatchOperation{
				{
					Op:    "add",
					Path:  "/image/svg",
					Value: base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><text>Valid</text></svg>`)),
				},
			},
			expectError: false,
		},
		{
			name: "add operation with invalid UTF-8 SVG",
			operations: []PatchOperation{
				{
					Op:    "add",
					Path:  "/image/svg",
					Value: base64.StdEncoding.EncodeToString([]byte{0x3C, 0x73, 0x76, 0x67, 0x3E, 0xa0, 0x3C, 0x2F, 0x73, 0x76, 0x67, 0x3E}),
				},
			},
			expectError: true,
			errorMsg:    "SVG data contains invalid UTF-8 sequences",
		},
		{
			name: "remove operation should not be processed",
			operations: []PatchOperation{
				{
					Op:   "remove",
					Path: "/image/svg",
				},
			},
			expectError: false,
		},
		{
			name: "non-string value should not be processed",
			operations: []PatchOperation{
				{
					Op:    "replace",
					Path:  "/image/svg",
					Value: 12345, // non-string value
				},
			},
			expectError: false,
		},
		{
			name: "empty SVG should be valid",
			operations: []PatchOperation{
				{
					Op:    "replace",
					Path:  "/image/svg",
					Value: base64.StdEncoding.EncodeToString([]byte("")),
				},
			},
			expectError: false,
		},
		{
			name: "valid UTF-8 with unicode characters",
			operations: []PatchOperation{
				{
					Op:    "replace",
					Path:  "/image/svg",
					Value: base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><text>Valid 中文 🚀</text></svg>`)),
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := preprocessPatchOperations(tt.operations)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Errorf("Expected error message '%s', got '%s'", tt.errorMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// For successful cases, verify the result
			if len(result) != len(tt.operations) {
				t.Errorf("Expected %d operations, got %d", len(tt.operations), len(result))
				return
			}

			// Check that SVG operations were properly processed
			for i, op := range tt.operations {
				if op.Path == "/image/svg" && (op.Op == "replace" || op.Op == "add") {
					if _, ok := op.Value.(string); ok {
						// Verify that the processed operation has the decoded bytes
						if _, ok := result[i].Value.([]byte); !ok {
							t.Errorf("Expected processed SVG operation to have []byte value, got %T", result[i].Value)
						}
					}
				} else {
					// Non-SVG operations should remain unchanged
					if result[i].Value != op.Value {
						t.Errorf("Non-SVG operation value changed from %v to %v", op.Value, result[i].Value)
					}
				}
			}
		})
	}
}

func TestPreprocessPatchOperations_MultipleOperations(t *testing.T) {
	// Test with multiple operations in a single request
	operations := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/name",
			Value: "Test Name",
		},
		{
			Op:    "replace",
			Path:  "/image/svg",
			Value: base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><text>Valid</text></svg>`)),
		},
		{
			Op:    "add",
			Path:  "/description",
			Value: "Test Description",
		},
	}

	result, err := preprocessPatchOperations(operations)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("Expected 3 operations, got %d", len(result))
	}

	// First operation should be unchanged
	if result[0].Value != "Test Name" {
		t.Errorf("First operation changed unexpectedly")
	}

	// Second operation (SVG) should be processed to bytes
	if _, ok := result[1].Value.([]byte); !ok {
		t.Errorf("SVG operation should have []byte value, got %T", result[1].Value)
	}

	// Third operation should be unchanged
	if result[2].Value != "Test Description" {
		t.Errorf("Third operation changed unexpectedly")
	}
}

func TestPreprocessPatchOperations_InvalidUTF8InMixedOperations(t *testing.T) {
	// Test that invalid UTF-8 in one operation fails the entire batch
	operations := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/name",
			Value: "Valid Name",
		},
		{
			Op:    "replace",
			Path:  "/image/svg",
			Value: base64.StdEncoding.EncodeToString([]byte{0x3C, 0x73, 0x76, 0x67, 0x3E, 0xa0}), // Invalid UTF-8
		},
	}

	_, err := preprocessPatchOperations(operations)
	if err == nil {
		t.Fatal("Expected error due to invalid UTF-8, but got none")
	}

	if err.Error() != "SVG data contains invalid UTF-8 sequences" {
		t.Errorf("Expected UTF-8 validation error, got: %v", err)
	}
}
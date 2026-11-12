package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

const jsonNull = "null"

// flattenAnswerValue converts a JSON answer value to a plain string.
// Arrays of strings become comma-separated. Booleans and numbers become
// their string representations. Objects and mixed arrays become JSON strings.
// Null and nil become empty string.
func flattenAnswerValue(value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}

	// Try null
	if string(value) == jsonNull {
		return ""
	}

	// Try string
	var s string
	if err := json.Unmarshal(value, &s); err == nil {
		return s
	}

	// Try number (json.Number or float64)
	var f float64
	if err := json.Unmarshal(value, &f); err == nil {
		return fmt.Sprintf("%g", f)
	}

	// Try boolean
	var b bool
	if err := json.Unmarshal(value, &b); err == nil {
		return fmt.Sprintf("%t", b)
	}

	// Try array
	var arr []json.RawMessage
	if err := json.Unmarshal(value, &arr); err == nil {
		if len(arr) == 0 {
			return ""
		}
		strs := make([]string, 0, len(arr))
		allStrings := true
		for _, elem := range arr {
			var s string
			if err := json.Unmarshal(elem, &s); err != nil {
				allStrings = false
				break
			}
			strs = append(strs, s)
		}
		if allStrings {
			return strings.Join(strs, ", ")
		}
		return string(value)
	}

	return string(value)
}

// flattenAndSanitize flattens a JSON answer value to a string and sanitizes
// it via bluemonday to prevent injection attacks.
func flattenAndSanitize(value json.RawMessage) string {
	flat := flattenAnswerValue(value)
	return SanitizePlainText(flat)
}

// parseCollectionAnswer parses a paneldynamic array-of-objects answer into
// typed sub-resources (Asset, Document, Repository). Returns the successfully
// parsed items and any fallback metadata entries for incomplete objects.
func parseCollectionAnswer(collectionType string, answer json.RawMessage) (items []any, fallbackMetadata []Metadata) {
	logger := slogging.Get()

	var objects []map[string]any
	if err := json.Unmarshal(answer, &objects); err != nil {
		logger.Warn("collection answer for %q is not an array of objects, falling back to metadata", collectionType)
		fallbackMetadata = append(fallbackMetadata, Metadata{
			Key:   collectionType,
			Value: SanitizePlainText(flattenAnswerValue(answer)),
		})
		return nil, fallbackMetadata
	}

	for _, obj := range objects {
		switch collectionType {
		case "assets":
			name, _ := obj["name"].(string)
			assetType, _ := obj["type"].(string)
			if name == "" || assetType == "" {
				logger.Warn("incomplete asset object (missing name or type), falling back to metadata")
				for k, v := range obj {
					valBytes, _ := json.Marshal(v)
					fallbackMetadata = append(fallbackMetadata, Metadata{
						Key:   fmt.Sprintf("assets.%s", k),
						Value: SanitizePlainText(flattenAnswerValue(valBytes)),
					})
				}
				continue
			}
			asset := Asset{
				Name: SanitizePlainText(name),
				Type: AssetType(SanitizePlainText(assetType)),
			}
			if desc, ok := obj["description"].(string); ok && desc != "" {
				sanitized := SanitizePlainText(desc)
				asset.Description = &sanitized
			}
			items = append(items, asset)

		case "documents":
			name, _ := obj["name"].(string)
			uri, _ := obj["uri"].(string)
			if name == "" || uri == "" {
				logger.Warn("incomplete document object (missing name or uri), falling back to metadata")
				for k, v := range obj {
					valBytes, _ := json.Marshal(v)
					fallbackMetadata = append(fallbackMetadata, Metadata{
						Key:   fmt.Sprintf("documents.%s", k),
						Value: SanitizePlainText(flattenAnswerValue(valBytes)),
					})
				}
				continue
			}
			doc := Document{
				Name: SanitizePlainText(name),
				Uri:  SanitizePlainText(uri),
			}
			items = append(items, doc)

		case "repositories":
			name, _ := obj["name"].(string)
			uri, _ := obj["uri"].(string)
			if name == "" || uri == "" {
				logger.Warn("incomplete repository object (missing name or uri), falling back to metadata")
				for k, v := range obj {
					valBytes, _ := json.Marshal(v)
					fallbackMetadata = append(fallbackMetadata, Metadata{
						Key:   fmt.Sprintf("repositories.%s", k),
						Value: SanitizePlainText(flattenAnswerValue(valBytes)),
					})
				}
				continue
			}
			sanitizedName := SanitizePlainText(name)
			repo := Repository{
				Name: &sanitizedName,
				Uri:  SanitizePlainText(uri),
			}
			items = append(items, repo)

		default:
			logger.Warn("unrecognized collection type %q, falling back to metadata", collectionType)
			for k, v := range obj {
				valBytes, _ := json.Marshal(v)
				fallbackMetadata = append(fallbackMetadata, Metadata{
					Key:   fmt.Sprintf("%s.%s", collectionType, k),
					Value: SanitizePlainText(flattenAnswerValue(valBytes)),
				})
			}
		}
	}
	return items, fallbackMetadata
}

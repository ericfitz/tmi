package api

import (
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
)

// SurveyQuestion represents a leaf question extracted from SurveyJS JSON.
type SurveyQuestion struct {
	Name          string
	Type          string
	Title         *string
	MapsToTmField *string
}

// ExtractQuestions recursively extracts leaf questions from a SurveyJS survey_json object.
// Returns an error if the JSON structure is invalid or duplicate mapsToTmField values are found.
// Pass nil for logger to suppress warnings about skipped elements.
func ExtractQuestions(surveyJSON map[string]any, logger *slogging.Logger) ([]SurveyQuestion, error) {
	pagesRaw, ok := surveyJSON["pages"]
	if !ok {
		return nil, fmt.Errorf("survey_json must contain a 'pages' field")
	}
	pages, ok := pagesRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("survey_json 'pages' must be an array")
	}

	var questions []SurveyQuestion
	for _, pageRaw := range pages {
		page, ok := pageRaw.(map[string]any)
		if !ok {
			continue
		}
		elementsRaw, ok := page["elements"]
		if !ok {
			continue
		}
		elements, ok := elementsRaw.([]any)
		if !ok {
			continue
		}
		extracted := extractFromElements(elements, logger)
		questions = append(questions, extracted...)
	}

	// Check for duplicate mapsToTmField values
	seen := make(map[string]string) // mapsToTmField -> question name
	for _, q := range questions {
		if q.MapsToTmField != nil {
			if prev, exists := seen[*q.MapsToTmField]; exists {
				return nil, fmt.Errorf("duplicate mapsToTmField %q: questions %q and %q both map to the same field", *q.MapsToTmField, prev, q.Name)
			}
			seen[*q.MapsToTmField] = q.Name
		}
	}

	return questions, nil
}

// extractFromElements recursively extracts questions from a SurveyJS elements array.
func extractFromElements(elements []any, logger *slogging.Logger) []SurveyQuestion {
	var questions []SurveyQuestion
	for _, elemRaw := range elements {
		elem, ok := elemRaw.(map[string]any)
		if !ok {
			continue
		}

		elemType, _ := elem["type"].(string)
		elemName, _ := elem["name"].(string)

		// Skip elements without name or type
		if elemName == "" || elemType == "" {
			if logger != nil {
				logger.Debug("skipping survey element without name or type: %v", elem)
			}
			continue
		}

		switch elemType {
		case "panel":
			// Recurse into panel's elements
			if childElementsRaw, ok := elem["elements"]; ok {
				if childElements, ok := childElementsRaw.([]any); ok {
					questions = append(questions, extractFromElements(childElements, logger)...)
				}
			}
		case "paneldynamic":
			// Emit the paneldynamic itself as a question (answer is the full array)
			q := makeQuestion(elem)
			questions = append(questions, q)

			// Scan templateElements for mapsToTmField conflict detection only.
			if teRaw, ok := elem["templateElements"]; ok {
				if te, ok := teRaw.([]any); ok {
					for _, childRaw := range te {
						child, ok := childRaw.(map[string]any)
						if !ok {
							continue
						}
						if mapping, ok := child["mapsToTmField"].(string); ok && mapping != "" {
							childName, _ := child["name"].(string)
							if logger != nil {
								logger.Warn("mapsToTmField %q on paneldynamic child %q is not supported (dynamic panels produce arrays, not scalar values)", mapping, childName)
							}
						}
					}
				}
			}
		default:
			// Leaf question
			q := makeQuestion(elem)
			questions = append(questions, q)
		}
	}
	return questions
}

// makeQuestion creates a SurveyQuestion from a SurveyJS element map.
// Callers must ensure elem contains non-empty "name" and "type" string fields.
func makeQuestion(elem map[string]any) SurveyQuestion {
	name, _ := elem["name"].(string)
	elemType, _ := elem["type"].(string)
	q := SurveyQuestion{
		Name: name,
		Type: elemType,
	}
	if title, ok := elem["title"].(string); ok && title != "" {
		q.Title = &title
	}
	if mapping, ok := elem["mapsToTmField"].(string); ok && mapping != "" {
		q.MapsToTmField = &mapping
	}
	return q
}

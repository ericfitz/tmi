package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SurveyAnswerRow is the API-layer representation of a stored survey answer.
// AnswerValue is nil when the respondent did not answer the question.
type SurveyAnswerRow struct {
	ID             string
	ResponseID     string
	QuestionName   string
	QuestionType   string
	QuestionTitle  *string
	MapsToTmField  *string
	AnswerValue    json.RawMessage // nil when no answer was provided
	ResponseStatus string
	CreatedAt      time.Time
}

// SurveyAnswerStore defines operations for persisting extracted survey answers.
// Answers are fully replaced on every call to ExtractAndSave (atomic delete+insert).
type SurveyAnswerStore interface {
	// ExtractAndSave extracts questions from surveyJSON using ExtractQuestions,
	// pairs each with its answer from the answers map, and atomically replaces
	// all existing answers for responseID with the new set.
	ExtractAndSave(ctx context.Context, responseID string, surveyJSON map[string]any, answers map[string]any, responseStatus string) error

	// GetAnswers returns all answer rows for a response, in insertion order.
	GetAnswers(ctx context.Context, responseID string) ([]SurveyAnswerRow, error)

	// GetFieldMappings returns a map of tmField -> SurveyAnswerRow for all answers
	// whose question has a non-nil MapsToTmField.
	GetFieldMappings(ctx context.Context, responseID string) (map[string]SurveyAnswerRow, error)

	// DeleteByResponseID removes all answer rows for a response.
	DeleteByResponseID(ctx context.Context, responseID string) error
}

// buildAnswerRows pairs each extracted SurveyQuestion with its answer value
// from the answers map, marshalling the value to JSON.  If a question has no
// corresponding key in answers, AnswerValue is left nil.
func buildAnswerRows(responseID string, questions []SurveyQuestion, answers map[string]any, responseStatus string) ([]SurveyAnswerRow, error) {
	rows := make([]SurveyAnswerRow, 0, len(questions))
	now := time.Now().UTC()

	for _, q := range questions {
		row := SurveyAnswerRow{
			ID:             uuid.New().String(),
			ResponseID:     responseID,
			QuestionName:   q.Name,
			QuestionType:   q.Type,
			QuestionTitle:  q.Title,
			MapsToTmField:  q.MapsToTmField,
			ResponseStatus: responseStatus,
			CreatedAt:      now,
		}

		if val, ok := answers[q.Name]; ok {
			b, err := json.Marshal(val)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal answer for question %q: %w", q.Name, err)
			}
			row.AnswerValue = b
		}

		rows = append(rows, row)
	}

	return rows, nil
}

// ── GORM implementation ────────────────────────────────────────────────────

// GormSurveyAnswerStore implements SurveyAnswerStore backed by a GORM database.
type GormSurveyAnswerStore struct {
	db *gorm.DB
}

// NewGormSurveyAnswerStore creates a new GORM-backed SurveyAnswerStore.
func NewGormSurveyAnswerStore(db *gorm.DB) *GormSurveyAnswerStore {
	return &GormSurveyAnswerStore{db: db}
}

// ExtractAndSave implements SurveyAnswerStore.
func (s *GormSurveyAnswerStore) ExtractAndSave(ctx context.Context, responseID string, surveyJSON map[string]any, answers map[string]any, responseStatus string) error {
	logger := slogging.Get()
	logger.Debug("ExtractAndSave survey answers for response %s", responseID)

	questions, err := ExtractQuestions(surveyJSON, logger)
	if err != nil {
		return fmt.Errorf("failed to extract questions: %w", err)
	}

	rows, err := buildAnswerRows(responseID, questions, answers, responseStatus)
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete existing answers for this response
		if err := tx.Where("response_id = ?", responseID).Delete(&models.SurveyAnswer{}).Error; err != nil {
			return fmt.Errorf("failed to delete existing answers for response %s: %w", responseID, err)
		}

		// Insert new answers
		for i := range rows {
			model := toModelRow(&rows[i])
			if err := tx.Create(&model).Error; err != nil {
				return fmt.Errorf("failed to insert answer for question %q: %w", rows[i].QuestionName, err)
			}
		}

		return nil
	})
}

// GetAnswers implements SurveyAnswerStore.
func (s *GormSurveyAnswerStore) GetAnswers(ctx context.Context, responseID string) ([]SurveyAnswerRow, error) {
	var modelRows []models.SurveyAnswer
	if err := s.db.WithContext(ctx).
		Where("response_id = ?", responseID).
		Order("created_at ASC").
		Find(&modelRows).Error; err != nil {
		return nil, fmt.Errorf("failed to get answers for response %s: %w", responseID, err)
	}

	return toAnswerRows(modelRows), nil
}

// GetFieldMappings implements SurveyAnswerStore.
func (s *GormSurveyAnswerStore) GetFieldMappings(ctx context.Context, responseID string) (map[string]SurveyAnswerRow, error) {
	var modelRows []models.SurveyAnswer
	if err := s.db.WithContext(ctx).
		Where("response_id = ? AND maps_to_tm_field IS NOT NULL", responseID).
		Find(&modelRows).Error; err != nil {
		return nil, fmt.Errorf("failed to get field mappings for response %s: %w", responseID, err)
	}

	result := make(map[string]SurveyAnswerRow, len(modelRows))
	for i := range modelRows {
		row := toAnswerRow(&modelRows[i])
		if row.MapsToTmField != nil {
			result[*row.MapsToTmField] = row
		}
	}
	return result, nil
}

// DeleteByResponseID implements SurveyAnswerStore.
func (s *GormSurveyAnswerStore) DeleteByResponseID(ctx context.Context, responseID string) error {
	if err := s.db.WithContext(ctx).
		Where("response_id = ?", responseID).
		Delete(&models.SurveyAnswer{}).Error; err != nil {
		return fmt.Errorf("failed to delete answers for response %s: %w", responseID, err)
	}
	return nil
}

// toModelRow converts a SurveyAnswerRow to a models.SurveyAnswer for persistence.
func toModelRow(row *SurveyAnswerRow) models.SurveyAnswer {
	m := models.SurveyAnswer{
		ID:             row.ID,
		ResponseID:     row.ResponseID,
		QuestionName:   row.QuestionName,
		QuestionType:   row.QuestionType,
		QuestionTitle:  row.QuestionTitle,
		MapsToTmField:  row.MapsToTmField,
		ResponseStatus: row.ResponseStatus,
		CreatedAt:      row.CreatedAt,
	}
	if row.AnswerValue != nil {
		m.AnswerValue = models.JSONRaw(row.AnswerValue)
	}
	return m
}

// toAnswerRows converts a slice of models.SurveyAnswer to []SurveyAnswerRow.
func toAnswerRows(modelRows []models.SurveyAnswer) []SurveyAnswerRow {
	rows := make([]SurveyAnswerRow, 0, len(modelRows))
	for i := range modelRows {
		rows = append(rows, toAnswerRow(&modelRows[i]))
	}
	return rows
}

// toAnswerRow converts a single models.SurveyAnswer to a SurveyAnswerRow.
func toAnswerRow(m *models.SurveyAnswer) SurveyAnswerRow {
	row := SurveyAnswerRow{
		ID:             m.ID,
		ResponseID:     m.ResponseID,
		QuestionName:   m.QuestionName,
		QuestionType:   m.QuestionType,
		QuestionTitle:  m.QuestionTitle,
		MapsToTmField:  m.MapsToTmField,
		ResponseStatus: m.ResponseStatus,
		CreatedAt:      m.CreatedAt,
	}
	if m.AnswerValue != nil {
		row.AnswerValue = json.RawMessage(m.AnswerValue)
	}
	return row
}

// ── In-memory implementation (for unit tests) ──────────────────────────────

// inMemorySurveyAnswerStore implements SurveyAnswerStore using an in-memory map.
type inMemorySurveyAnswerStore struct {
	mu   sync.RWMutex
	data map[string][]SurveyAnswerRow // keyed by responseID
}

// newInMemorySurveyAnswerStore creates a new in-memory SurveyAnswerStore.
func newInMemorySurveyAnswerStore() *inMemorySurveyAnswerStore {
	return &inMemorySurveyAnswerStore{
		data: make(map[string][]SurveyAnswerRow),
	}
}

// ExtractAndSave implements SurveyAnswerStore.
func (s *inMemorySurveyAnswerStore) ExtractAndSave(ctx context.Context, responseID string, surveyJSON map[string]any, answers map[string]any, responseStatus string) error {
	questions, err := ExtractQuestions(surveyJSON, nil)
	if err != nil {
		return fmt.Errorf("failed to extract questions: %w", err)
	}

	rows, err := buildAnswerRows(responseID, questions, answers, responseStatus)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[responseID] = rows
	return nil
}

// GetAnswers implements SurveyAnswerStore.
func (s *inMemorySurveyAnswerStore) GetAnswers(_ context.Context, responseID string) ([]SurveyAnswerRow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := s.data[responseID]
	// Return a copy to avoid mutations of internal state
	result := make([]SurveyAnswerRow, len(rows))
	copy(result, rows)
	return result, nil
}

// GetFieldMappings implements SurveyAnswerStore.
func (s *inMemorySurveyAnswerStore) GetFieldMappings(ctx context.Context, responseID string) (map[string]SurveyAnswerRow, error) {
	rows, err := s.GetAnswers(ctx, responseID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]SurveyAnswerRow)
	for _, row := range rows {
		if row.MapsToTmField != nil {
			result[*row.MapsToTmField] = row
		}
	}
	return result, nil
}

// DeleteByResponseID implements SurveyAnswerStore.
func (s *inMemorySurveyAnswerStore) DeleteByResponseID(_ context.Context, responseID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, responseID)
	return nil
}

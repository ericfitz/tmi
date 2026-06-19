// Package api — store-side helpers for the optimistic-locking contract
// (T14 / #385). Each Gorm-backed store for a versioned entity exposes a
// CheckAndBumpVersion method that delegates to the central helper. Handlers
// call this BEFORE issuing the entity-specific Update so concurrent writers
// race on a single CAS-style UPDATE: the first to commit wins, the loser
// gets ErrVersionMismatch.
//
// Each store passes its model's TableName() to the central helper so Oracle
// (uppercase) and PostgreSQL (lowercase) identifier folding both resolve to
// the right table name without leaking schema knowledge here.
package api

import (
	"context"

	"github.com/ericfitz/tmi/api/models"
)

// CheckAndBumpVersion atomically validates and increments the threat model
// row's version. See optimistic_locking.go::CheckAndBumpVersion for semantics.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: atomically validate and increment the threat model row's optimistic-lock version (reads DB)
func (s *GormThreatModelStore) CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error) {
	return CheckAndBumpVersion(ctx, s.db, models.ThreatModel{}.TableName(), id, expected)
}

// CheckAndBumpVersion atomically validates and increments the diagram row's
// version.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: atomically validate and increment the diagram row's optimistic-lock version (reads DB)
func (s *GormDiagramStore) CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error) {
	return CheckAndBumpVersion(ctx, s.db, models.Diagram{}.TableName(), id, expected)
}

// CheckAndBumpVersion atomically validates and increments the asset row's
// version.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: atomically validate and increment the asset row's optimistic-lock version (reads DB)
func (s *GormAssetRepository) CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error) {
	return CheckAndBumpVersion(ctx, s.db, models.Asset{}.TableName(), id, expected)
}

// CheckAndBumpVersion atomically validates and increments the threat row's
// version.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: atomically validate and increment the threat row's optimistic-lock version (reads DB)
func (s *GormThreatRepository) CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error) {
	return CheckAndBumpVersion(ctx, s.db, models.Threat{}.TableName(), id, expected)
}

// CheckAndBumpVersion atomically validates and increments the document row's
// version.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: atomically validate and increment the document row's optimistic-lock version (reads DB)
func (s *GormDocumentRepository) CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error) {
	return CheckAndBumpVersion(ctx, s.db, models.Document{}.TableName(), id, expected)
}

// CheckAndBumpVersion atomically validates and increments the team row's
// version.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: atomically validate and increment the team row's optimistic-lock version (reads DB)
func (s *GormTeamStore) CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error) {
	return CheckAndBumpVersion(ctx, s.db, models.TeamRecord{}.TableName(), id, expected)
}

// CheckAndBumpVersion atomically validates and increments the project row's
// version.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: atomically validate and increment the project row's optimistic-lock version (reads DB)
func (s *GormProjectStore) CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error) {
	return CheckAndBumpVersion(ctx, s.db, models.ProjectRecord{}.TableName(), id, expected)
}

// CheckAndBumpVersion atomically validates and increments the survey response
// row's version.
// SEM@3253a9999eeaddc59fa7469d4f7d7fe80d59c6ca: atomically validate and increment the survey response row's optimistic-lock version (reads DB)
func (s *GormSurveyResponseStore) CheckAndBumpVersion(ctx context.Context, id string, expected int) (int, error) {
	return CheckAndBumpVersion(ctx, s.db, models.SurveyResponse{}.TableName(), id, expected)
}

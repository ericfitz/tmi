package auth

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/ericfitz/tmi/internal/slogging"
)

// TransferResult contains statistics about the ownership transfer operation
type TransferResult struct {
	ThreatModelIDs    []string `json:"threat_model_ids"`
	SurveyResponseIDs []string `json:"survey_response_ids"`
}

// TransferOwnership transfers all owned threat models and survey responses
// from one user to another. The source user retains "writer" access.
func (s *Service) TransferOwnership(ctx context.Context, sourceUserUUID, targetUserUUID string) (*TransferResult, error) {
	if sourceUserUUID == targetUserUUID {
		return nil, fmt.Errorf("cannot transfer ownership to self")
	}

	repoResult, err := s.deletionRepo.TransferOwnership(ctx, sourceUserUUID, targetUserUUID)
	if err != nil {
		if err == repository.ErrUserNotFound {
			return nil, err
		}
		slogging.Get().Error("Failed to transfer ownership from %s to %s: %v", sourceUserUUID, targetUserUUID, err)
		return nil, fmt.Errorf("failed to transfer ownership: %w", err)
	}

	return &TransferResult{
		ThreatModelIDs:    repoResult.ThreatModelIDs,
		SurveyResponseIDs: repoResult.SurveyResponseIDs,
	}, nil
}

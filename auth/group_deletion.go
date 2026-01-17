package auth

import (
	"context"
)

// GroupDeletionResult contains statistics about the group deletion operation
type GroupDeletionResult struct {
	ThreatModelsDeleted  int    `json:"threat_models_deleted"`
	ThreatModelsRetained int    `json:"threat_models_retained"`
	GroupName            string `json:"group_name"`
}

// DeleteGroupAndData deletes a TMI-managed group and handles threat model cleanup
// Groups are always provider-independent (provider="*") in TMI
func (s *Service) DeleteGroupAndData(ctx context.Context, groupName string) (*GroupDeletionResult, error) {
	repoResult, err := s.deletionRepo.DeleteGroupAndData(ctx, groupName)
	if err != nil {
		return nil, err
	}

	return &GroupDeletionResult{
		ThreatModelsDeleted:  repoResult.ThreatModelsDeleted,
		ThreatModelsRetained: repoResult.ThreatModelsRetained,
		GroupName:            repoResult.GroupName,
	}, nil
}

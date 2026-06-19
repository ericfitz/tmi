package auth

import (
	"context"
)

// GroupDeletionResult contains statistics about the group deletion operation
// SEM@9f44e79e7d62c7dfd68d7566466e5b7ddab1f34a: counts of threat models deleted and retained when a group is removed (pure)
type GroupDeletionResult struct {
	ThreatModelsDeleted  int    `json:"threat_models_deleted"`
	ThreatModelsRetained int    `json:"threat_models_retained"`
	GroupName            string `json:"group_name"`
}

// DeleteGroupAndData deletes a TMI-managed group by internal UUID and handles threat model cleanup
// Uses internal_uuid for precise identification to avoid issues with duplicate group_names
// SEM@96488469dcfa20f1b615dc581cdcefa18cb974ae: delete a TMI-managed group by internal UUID and cascade threat model cleanup (reads DB)
func (s *Service) DeleteGroupAndData(ctx context.Context, internalUUID string) (*GroupDeletionResult, error) {
	repoResult, err := s.deletionRepo.DeleteGroupAndData(ctx, internalUUID)
	if err != nil {
		return nil, err
	}

	return &GroupDeletionResult{
		ThreatModelsDeleted:  repoResult.ThreatModelsDeleted,
		ThreatModelsRetained: repoResult.ThreatModelsRetained,
		GroupName:            repoResult.GroupName,
	}, nil
}

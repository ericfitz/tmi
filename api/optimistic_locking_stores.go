// Package api — store-side contracts for the optimistic-locking write path
// (T14 / #385, same-transaction fix #594).
//
// A versioned entity store implements UpdateWithVersion(ctx, ..., expected
// int) alongside its plain Update. UpdateWithVersion opens one retryable
// transaction and, as its first statement, calls the central
// CheckAndBumpVersion helper on the tx handle before issuing the content
// UPDATE — so the CAS and the write it guards commit or roll back together.
// A concurrent writer either blocks on the CAS's row lock until the first
// transaction commits (then sees a version mismatch), or loses the CAS
// outright; it can never observe a stale version and interleave its content
// write with the winner's, which was possible before #594.
//
// Handlers cannot call UpdateWithVersion through a common interface because
// each entity's Update method has its own parameter and result shape, so
// this file declares one narrow interface per entity. Handlers type-assert
// the package-level store globals (typed as broader interfaces) against the
// matching interface here to reach UpdateWithVersion without introducing
// circular references.
package api

import (
	"context"
)

// versionedThreatModelUpdater is implemented by stores that can update a
// threat model guarded by a same-transaction optimistic-lock CAS.
type versionedThreatModelUpdater interface {
	UpdateWithVersion(ctx context.Context, id string, item ThreatModel, expectedVersion int) (int, error)
}

// versionedDiagramUpdater is implemented by stores that can update a diagram
// guarded by a same-transaction optimistic-lock CAS.
type versionedDiagramUpdater interface {
	UpdateWithVersion(ctx context.Context, id string, item DfdDiagram, expectedVersion int) (int, error)
}

// versionedAssetUpdater is implemented by stores that can update an asset
// guarded by a same-transaction optimistic-lock CAS.
type versionedAssetUpdater interface {
	UpdateWithVersion(ctx context.Context, asset *Asset, threatModelID string, expectedVersion int) (int, error)
}

// versionedAssetPatcher is implemented by stores that can apply JSON Patch
// operations to an asset guarded by a same-transaction optimistic-lock CAS.
type versionedAssetPatcher interface {
	PatchWithVersion(ctx context.Context, id string, operations []PatchOperation, expectedVersion int) (*Asset, int, error)
}

// versionedThreatUpdater is implemented by stores that can update a threat
// guarded by a same-transaction optimistic-lock CAS.
type versionedThreatUpdater interface {
	UpdateWithVersion(ctx context.Context, threat *Threat, expectedVersion int) (int, error)
}

// versionedThreatPatcher is implemented by stores that can apply JSON Patch
// operations to a threat guarded by a same-transaction optimistic-lock CAS.
type versionedThreatPatcher interface {
	PatchWithVersion(ctx context.Context, threatModelID string, id string, operations []PatchOperation, expectedVersion int) (*Threat, int, error)
}

// versionedDocumentUpdater is implemented by stores that can update a
// document guarded by a same-transaction optimistic-lock CAS.
type versionedDocumentUpdater interface {
	UpdateWithVersion(ctx context.Context, document *Document, threatModelID string, expectedVersion int) (int, error)
}

// versionedDocumentPatcher is implemented by stores that can apply JSON
// Patch operations to a document guarded by a same-transaction
// optimistic-lock CAS.
type versionedDocumentPatcher interface {
	PatchWithVersion(ctx context.Context, id string, operations []PatchOperation, expectedVersion int) (*Document, int, error)
}

// versionedTeamUpdater is implemented by stores that can update a team
// guarded by a same-transaction optimistic-lock CAS.
type versionedTeamUpdater interface {
	UpdateWithVersion(ctx context.Context, id string, team *Team, userInternalUUID string, expectedVersion int) (*Team, int, error)
}

// versionedProjectUpdater is implemented by stores that can update a project
// guarded by a same-transaction optimistic-lock CAS.
type versionedProjectUpdater interface {
	UpdateWithVersion(ctx context.Context, id string, project *Project, userInternalUUID string, expectedVersion int) (*Project, int, error)
}

// versionedSurveyResponseUpdater is implemented by stores that can update a
// survey response guarded by a same-transaction optimistic-lock CAS.
type versionedSurveyResponseUpdater interface {
	UpdateWithVersion(ctx context.Context, response *SurveyResponse, expectedVersion int) (int, error)
}

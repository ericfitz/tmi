// api/timmy_entity_key.go
package api

// EntityKey identifies a single chunked entity within a threat model. It is
// the natural map key for "one row per entity" lookups (e.g., the freshness
// metadata used by prepareVectorIndex).
type EntityKey struct {
	EntityType string
	EntityID   string
}

// EntityEmbeddingMeta is the per-entity tuple needed to decide whether
// existing embeddings are still usable without loading the vectors.
// Hash, Model, and Dim are taken from any one row of the entity's chunks
// (they are identical across an entity's chunks by construction in
// CreateBatch).
type EntityEmbeddingMeta struct {
	ContentHash    string
	EmbeddingModel string
	EmbeddingDim   int
}

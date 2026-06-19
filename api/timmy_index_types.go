package api

const (
	// IndexTypeText is the index type for text content (assets, threats, diagrams, documents, notes)
	IndexTypeText = "text"

	// IndexTypeCode is the index type for code content (repositories)
	IndexTypeCode = "code"
)

// EntityTypeToIndexType maps an entity type to its vector index type.
// Repositories go to the code index; everything else goes to the text index.
// SEM@37a05c9c7bcde023781ade490d31e55313f5a793: convert an entity type string to its search index type, mapping repositories to code index (pure)
func EntityTypeToIndexType(entityType string) string {
	if entityType == string(AuditEntryObjectTypeRepository) {
		return IndexTypeCode
	}
	return IndexTypeText
}

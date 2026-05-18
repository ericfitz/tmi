package extract

// ContentExtractorRegistry manages content extractors in priority order.
type ContentExtractorRegistry struct {
	extractors []ContentExtractor
}

// NewContentExtractorRegistry creates a new registry.
func NewContentExtractorRegistry() *ContentExtractorRegistry {
	return &ContentExtractorRegistry{}
}

// Register adds an extractor to the registry.
func (r *ContentExtractorRegistry) Register(extractor ContentExtractor) {
	r.extractors = append(r.extractors, extractor)
}

// FindExtractor returns the first extractor that can handle the given content type.
func (r *ContentExtractorRegistry) FindExtractor(contentType string) (ContentExtractor, bool) {
	for _, e := range r.extractors {
		if e.CanHandle(contentType) {
			return e, true
		}
	}
	return nil, false
}

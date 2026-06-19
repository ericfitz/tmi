package extract

// ContentExtractorRegistry manages content extractors in priority order.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: ordered registry of content extractors matched by content type (pure)
type ContentExtractorRegistry struct {
	extractors []ContentExtractor
}

// NewContentExtractorRegistry creates a new registry.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: build an empty content extractor registry (pure)
func NewContentExtractorRegistry() *ContentExtractorRegistry {
	return &ContentExtractorRegistry{}
}

// Register adds an extractor to the registry.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: register a content extractor at the end of the priority list (mutates shared state)
func (r *ContentExtractorRegistry) Register(extractor ContentExtractor) {
	r.extractors = append(r.extractors, extractor)
}

// FindExtractor returns the first extractor that can handle the given content type.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: return the first registered extractor that can handle the given content type (pure)
func (r *ContentExtractorRegistry) FindExtractor(contentType string) (ContentExtractor, bool) {
	for _, e := range r.extractors {
		if e.CanHandle(contentType) {
			return e, true
		}
	}
	return nil, false
}

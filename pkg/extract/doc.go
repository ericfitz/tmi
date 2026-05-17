// Package extract is the framework-free document/content extraction
// library. It imports nothing from the monolith — no Gin, no GORM, no
// internal/config — so it can be linked into the sandboxed tmi-extractor
// worker and into the monolith's api package alike.
//
// This package holds the foundation types relocated from package api
// during TMI Component Platform Plan 2 (issue #347): the extractor
// interfaces (ContentExtractor, ContextAwareExtractor, BoundedExtractor),
// the extraction result and entity-reference types, the sentinel errors
// and reason-code constants, the typed-error classifier (ClassifyError),
// and the extraction Limits struct with DefaultLimits. The extractor
// registry and the OOXML/PDF/HTML/plaintext extractors were
// relocated here by Plan 2 Task 6.
//
// The move is a relocation, not a rewrite: the extraction logic is
// byte-for-byte the same; only the package boundary, the import paths,
// and the exported-identifier surface changed.
package extract

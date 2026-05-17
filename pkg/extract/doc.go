// Package extract is the framework-free document/content extraction
// library: the extractor registry, the OOXML/PDF/HTML/plaintext extractors,
// the bounded-extractor and wall-clock-budget machinery, and the typed
// error classification. It imports nothing from the monolith — no Gin, no
// GORM, no internal/config — so it can be linked into the sandboxed
// tmi-extractor worker and into the monolith's api package alike.
//
// This package was relocated from package api during TMI Component Platform
// Plan 2 (issue #347). The move is a relocation, not a rewrite: the
// extraction logic is byte-for-byte the same; only the package boundary,
// the import paths, and the exported-identifier surface changed.
package extract

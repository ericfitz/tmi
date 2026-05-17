// Package jobenvelope defines the single job-envelope schema used by every
// stage of the TMI Component Platform extraction pipeline. One shape — no
// discriminator — flows from the monolith through tmi-extractor and
// tmi-chunk-embed back to the monolith's result-consumer. Input mode is
// declared per-component (content-ref vs source-locator); the monolith
// populates the matching Input fields. source-locator fields are RESERVED
// for the future code extractor and are not exercised by issue #347.
package jobenvelope

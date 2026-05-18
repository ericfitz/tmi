package jobenvelope

import "fmt"

// Validate checks a Job against the envelope contract. It rejects a missing
// job_id, a missing content_type, an empty Input, and an Input that sets
// both the content-ref and source-locator modes at once. It does NOT
// reject source-locator alone — that mode is RESERVED but schema-valid.
func Validate(j Job) error {
	if j.JobID == "" {
		return fmt.Errorf("jobenvelope: job_id is required")
	}
	if j.ContentType == "" {
		return fmt.Errorf("jobenvelope: content_type is required")
	}
	hasContentRef := j.Input.ObjectRef != ""
	hasSourceLocator := j.Input.SourceURL != ""
	switch {
	case hasContentRef && hasSourceLocator:
		return fmt.Errorf("jobenvelope: input sets both content-ref and source-locator")
	case !hasContentRef && !hasSourceLocator:
		return fmt.Errorf("jobenvelope: input has neither object_ref nor source_url")
	}
	return nil
}

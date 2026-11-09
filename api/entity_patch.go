package api

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// copyMovePatchBlockedTargets are the free-text and URI fields the handlers
// sanitize (SanitizePlainText) and URI-validate on add and replace only. The
// store cannot reach the handler's URI validators, so copy and move, which
// would carry unsanitized text or an unvalidated URI into one of these fields,
// are rejected there instead; add and replace remain available.
var copyMovePatchBlockedTargets = map[string]bool{
	PatchPathName:        true,
	PatchPathDescription: true,
	"/mitigation":        true,
	"/issue_uri":         true,
	PatchPathURI:         true,
}

// applyEntityPatch applies RFC 6902 operations to a sub-resource entity with
// the same engine every other resource uses (add on an existing member
// replaces; replace on an unset optional field is promoted to add; copy, move
// and test work), after checking that every operation writes only paths in
// patchable: exactly the fields the store's update persists. A patch that
// would change any other field is rejected with 400 rather than silently
// dropped on save (#1143). The PUT validators registered under
// validationConfig then run on the patched entity so a patch cannot store
// what a PUT would reject. Errors are 400 RequestErrors.
// SEM@0366350593b297b3143d1125e29982be617236ab: apply RFC 6902 operations to an entity restricted to well-formed persisted-field paths, rejecting copy/move into sanitized text and URI fields, then run the PUT validators, failing closed (pure)
func applyEntityPatch[T any](entity T, operations []PatchOperation, noun string, patchable map[string]bool, validationConfig string) (T, error) {
	var zero T
	for _, op := range operations {
		// paths lists every pointer the operation reads or writes: writes need
		// the allowlist, and reads (copy/move "from", test "path") must not
		// reach server-managed fields or the document root either.
		var paths []string
		switch op.Op {
		case string(Add), string(Replace), string(Remove), string(Test):
			paths = []string{op.Path}
		case string(Copy), string(Move):
			if copyMovePatchBlockedTargets[topLevelPatchPath(op.Path)] {
				return zero, InvalidInputError(fmt.Sprintf(
					"patch operation %q may not target %s; use add or replace with the value so it is sanitized and validated",
					op.Op, op.Path))
			}
			paths = []string{op.Path, op.From}
		default:
			return zero, InvalidInputError(fmt.Sprintf("unsupported patch operation %q", op.Op))
		}
		for _, path := range paths {
			if !wellFormedPatchPointer(path) || !patchable[topLevelPatchPath(path)] {
				return zero, InvalidInputError(fmt.Sprintf(
					"patch operation %q may not use path %q; %s patches may change %s",
					op.Op, path, noun, patchablePathList(patchable)))
			}
		}
	}

	patched, err := ApplyPatchOperations(entity, operations)
	if err != nil {
		return zero, err
	}

	// Fail closed: a missing config would otherwise skip every validator.
	cfg, ok := ValidationConfigs[validationConfig]
	if !ok {
		return zero, fmt.Errorf("no validation config %q registered for %s patches", validationConfig, noun)
	}
	for _, validator := range cfg.CustomValidators {
		if verr := validator(&patched); verr != nil {
			var reqErr *RequestError
			if errors.As(verr, &reqErr) {
				return zero, reqErr
			}
			return zero, InvalidInputError(verr.Error())
		}
	}
	return patched, nil
}

// patchablePathList renders the sorted patchable field names for error text.
// SEM@4a5010eb1b18de9154f1d8440292237603753fd2: build a sorted comma-separated list of patchable field names (pure)
func patchablePathList(patchable map[string]bool) string {
	names := make([]string, 0, len(patchable))
	for p := range patchable {
		names = append(names, strings.TrimPrefix(p, "/"))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// defaultTrue sets an unset (nil) flag to true, the schema default for
// include_in_report and timmy_enabled, so a patch that removes the flag
// restores the default instead of letting update write false.
// SEM@4a5010eb1b18de9154f1d8440292237603753fd2: set a nil boolean flag to its schema default of true (pure)
func defaultTrue(flag **bool) {
	if *flag == nil {
		t := true
		*flag = &t
	}
}

// wellFormedPatchPointer reports whether path is a non-root JSON Pointer with
// no empty segments. topLevelPatchPath folds "/ssvc/" to "/ssvc", so without
// this check a trailing-slash or "//" path passes the allowlist and addresses
// the empty-named member, a silent no-op.
// SEM@0366350593b297b3143d1125e29982be617236ab: check that a JSON Pointer is non-root and has no empty segments (pure)
func wellFormedPatchPointer(path string) bool {
	if len(path) < 2 || path[0] != '/' {
		return false
	}
	return !strings.HasSuffix(path, "/") && !strings.Contains(path, "//")
}

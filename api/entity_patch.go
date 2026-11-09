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
// SEM@88d573e459d9015f908f5783c185514054038eec: apply RFC 6902 operations to an entity restricted to persisted fields, rejecting copy/move into sanitized text and URI fields, then run the PUT validators (pure)
func applyEntityPatch[T any](entity T, operations []PatchOperation, noun string, patchable map[string]bool, validationConfig string) (T, error) {
	var zero T
	for _, op := range operations {
		var writes []string
		switch op.Op {
		case string(Add), string(Replace), string(Remove):
			writes = []string{op.Path}
		case string(Copy), string(Move):
			if copyMovePatchBlockedTargets[topLevelPatchPath(op.Path)] {
				return zero, InvalidInputError(fmt.Sprintf(
					"patch operation %q may not target %s; use add or replace with the value so it is sanitized and validated",
					op.Op, op.Path))
			}
			writes = []string{op.Path}
			if op.Op == string(Move) {
				writes = append(writes, op.From)
			}
		case string(Test):
		default:
			return zero, InvalidInputError(fmt.Sprintf("unsupported patch operation %q", op.Op))
		}
		for _, path := range writes {
			if !patchable[topLevelPatchPath(path)] {
				return zero, InvalidInputError(fmt.Sprintf(
					"patch operation %q may not change %s; %s patches may change %s",
					op.Op, path, noun, patchablePathList(patchable)))
			}
		}
	}

	patched, err := ApplyPatchOperations(entity, operations)
	if err != nil {
		return zero, err
	}

	if cfg, ok := ValidationConfigs[validationConfig]; ok {
		for _, validator := range cfg.CustomValidators {
			if verr := validator(&patched); verr != nil {
				var reqErr *RequestError
				if errors.As(verr, &reqErr) {
					return zero, reqErr
				}
				return zero, InvalidInputError(verr.Error())
			}
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

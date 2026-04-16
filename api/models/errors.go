package models

import "errors"

// ErrBuiltInGroupProtected is returned by GORM hooks and repositories when an
// operation would modify or delete a built-in group (everyone, security-reviewers,
// administrators). Handlers use errors.Is(err, ErrBuiltInGroupProtected) to map
// these conditions to HTTP 403 responses.
var ErrBuiltInGroupProtected = errors.New("built-in group is protected")

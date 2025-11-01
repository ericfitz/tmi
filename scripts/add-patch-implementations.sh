#!/bin/bash
# Add Patch() method implementations to Document, Note, and Repository stores

set -e

echo "Adding Patch() implementations to remaining stores..."

# Document Store
cat >> api/document_store.go << 'EOF'

// Patch applies JSON patch operations to a document
func (s *DatabaseDocumentStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Document, error) {
	logger := slogging.Get()
	logger.Debug("Patching document %s with %d operations", id, len(operations))

	// Get current document
	document, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	for _, op := range operations {
		if err := s.applyPatchOperation(document, op); err != nil {
			logger.Error("Failed to apply patch operation %s to document %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Get threat model ID for update
	threatModelID, err := s.getDocumentThreatModelID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model ID: %w", err)
	}

	// Update the document
	if err := s.Update(ctx, document, threatModelID); err != nil {
		return nil, err
	}

	return document, nil
}

// applyPatchOperation applies a single patch operation to a document
func (s *DatabaseDocumentStore) applyPatchOperation(document *Document, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				document.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/uri":
		if op.Op == "replace" {
			if uri, ok := op.Value.(string); ok {
				document.Uri = uri
			} else {
				return fmt.Errorf("invalid value type for uri: expected string")
			}
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				document.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case "remove":
			document.Description = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getDocumentThreatModelID retrieves the threat model ID for a document
func (s *DatabaseDocumentStore) getDocumentThreatModelID(ctx context.Context, documentID string) (string, error) {
	query := `SELECT threat_model_id FROM documents WHERE id = $1`
	var threatModelID string
	err := s.db.QueryRowContext(ctx, query, documentID).Scan(&threatModelID)
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for document: %w", err)
	}
	return threatModelID, nil
}
EOF

echo "  - Added Patch() to DocumentStore"

# Note Store - first add to interface
# Then add implementation
cat >> api/note_store.go << 'EOF'

// Patch applies JSON patch operations to a note
func (s *DatabaseNoteStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Note, error) {
	logger := slogging.Get()
	logger.Debug("Patching note %s with %d operations", id, len(operations))

	// Get current note
	note, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	for _, op := range operations {
		if err := s.applyPatchOperation(note, op); err != nil {
			logger.Error("Failed to apply patch operation %s to note %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Get threat model ID for update
	threatModelID, err := s.getNoteThreatModelID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model ID: %w", err)
	}

	// Update the note
	if err := s.Update(ctx, note, threatModelID); err != nil {
		return nil, err
	}

	return note, nil
}

// applyPatchOperation applies a single patch operation to a note
func (s *DatabaseNoteStore) applyPatchOperation(note *Note, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				note.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/content":
		switch op.Op {
		case "replace", "add":
			if content, ok := op.Value.(string); ok {
				note.Content = &content
			} else {
				return fmt.Errorf("invalid value type for content: expected string")
			}
		case "remove":
			note.Content = nil
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				note.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case "remove":
			note.Description = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getNoteThreatModelID retrieves the threat model ID for a note
func (s *DatabaseNoteStore) getNoteThreatModelID(ctx context.Context, noteID string) (string, error) {
	query := `SELECT threat_model_id FROM notes WHERE id = $1`
	var threatModelID string
	err := s.db.QueryRowContext(ctx, query, noteID).Scan(&threatModelID)
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for note: %w", err)
	}
	return threatModelID, nil
}
EOF

echo "  - Added Patch() to NoteStore"

# Repository Store
cat >> api/repository_store.go << 'EOF'

// Patch applies JSON patch operations to a repository
func (s *DatabaseRepositoryStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Repository, error) {
	logger := slogging.Get()
	logger.Debug("Patching repository %s with %d operations", id, len(operations))

	// Get current repository
	repository, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	for _, op := range operations {
		if err := s.applyPatchOperation(repository, op); err != nil {
			logger.Error("Failed to apply patch operation %s to repository %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Get threat model ID for update
	threatModelID, err := s.getRepositoryThreatModelID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model ID: %w", err)
	}

	// Update the repository
	if err := s.Update(ctx, repository, threatModelID); err != nil {
		return nil, err
	}

	return repository, nil
}

// applyPatchOperation applies a single patch operation to a repository
func (s *DatabaseRepositoryStore) applyPatchOperation(repository *Repository, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				repository.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/type":
		if op.Op == "replace" {
			if repoType, ok := op.Value.(string); ok {
				repository.Type = repoType
			} else {
				return fmt.Errorf("invalid value type for type: expected string")
			}
		}
	case "/uri":
		if op.Op == "replace" {
			if uri, ok := op.Value.(string); ok {
				repository.Uri = uri
			} else {
				return fmt.Errorf("invalid value type for uri: expected string")
			}
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				repository.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case "remove":
			repository.Description = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getRepositoryThreatModelID retrieves the threat model ID for a repository
func (s *DatabaseRepositoryStore) getRepositoryThreatModelID(ctx context.Context, repositoryID string) (string, error) {
	query := `SELECT threat_model_id FROM repositories WHERE id = $1`
	var threatModelID string
	err := s.db.QueryRowContext(ctx, query, repositoryID).Scan(&threatModelID)
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for repository: %w", err)
	}
	return threatModelID, nil
}
EOF

echo "  - Added Patch() to RepositoryStore"

echo "Done! All Patch() implementations added."

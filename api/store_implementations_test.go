package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInMemoryDocumentStore tests the in-memory document store implementation
func TestInMemoryDocumentStore(t *testing.T) {
	ctx := context.Background()

	t.Run("Create and Get", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		threatModelID := uuid.New().String()
		doc := &Document{
			Name: "Test Document",
			Url:  "https://example.com/doc",
		}
		desc := "Test description"
		doc.Description = &desc

		err := store.Create(ctx, doc, threatModelID)
		require.NoError(t, err)

		// The Create method should set the ID via the idSetter function
		// But we need to get the created document from the store since
		// the original document pointer may not be modified

		// List all documents to find the one we just created
		docs, err := store.List(ctx, threatModelID, 0, 10)
		require.NoError(t, err)
		require.Len(t, docs, 1)

		createdDoc := docs[0]
		assert.NotNil(t, createdDoc.Id)
		assert.Equal(t, doc.Name, createdDoc.Name)

		retrieved, err := store.Get(ctx, createdDoc.Id.String())
		require.NoError(t, err)
		assert.Equal(t, createdDoc.Name, retrieved.Name)
		assert.Equal(t, createdDoc.Url, retrieved.Url)
		assert.Equal(t, *createdDoc.Description, *retrieved.Description)
		assert.Equal(t, createdDoc.Id, retrieved.Id)
	})

	t.Run("Get non-existent document", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		_, err := store.Get(ctx, uuid.New().String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Update", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		threatModelID := uuid.New().String()
		doc := &Document{
			Name: "Original Document",
			Url:  "https://example.com/original",
		}

		err := store.Create(ctx, doc, threatModelID)
		require.NoError(t, err)

		// Get the created document to get the ID
		docs, err := store.List(ctx, threatModelID, 0, 10)
		require.NoError(t, err)
		require.Len(t, docs, 1)
		createdDoc := docs[0]

		// Update the created document
		createdDoc.Name = "Updated Document"
		createdDoc.Url = "https://example.com/updated"
		newDesc := "Updated description"
		createdDoc.Description = &newDesc

		err = store.Update(ctx, &createdDoc, threatModelID)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, createdDoc.Id.String())
		require.NoError(t, err)
		assert.Equal(t, "Updated Document", retrieved.Name)
		assert.Equal(t, "https://example.com/updated", retrieved.Url)
		assert.Equal(t, "Updated description", *retrieved.Description)
	})

	t.Run("Update non-existent document", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		threatModelID := uuid.New().String()
		doc := &Document{
			Id:   &[]uuid.UUID{uuid.New()}[0],
			Name: "Non-existent",
		}

		err := store.Update(ctx, doc, threatModelID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Delete", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		threatModelID := uuid.New().String()
		doc := &Document{
			Name: "To Delete",
			Url:  "https://example.com/delete",
		}

		err := store.Create(ctx, doc, threatModelID)
		require.NoError(t, err)

		// Get the created document to find the ID
		docs, err := store.List(ctx, threatModelID, 0, 10)
		require.NoError(t, err)
		require.Len(t, docs, 1)
		createdDoc := docs[0]

		err = store.Delete(ctx, createdDoc.Id.String())
		require.NoError(t, err)

		_, err = store.Get(ctx, createdDoc.Id.String())
		assert.Error(t, err)
	})

	t.Run("Delete non-existent document", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		err := store.Delete(ctx, uuid.New().String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("List documents", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		threatModelID := uuid.New().String()
		// Create test documents
		docs := []*Document{
			{Name: "Doc 1", Url: "https://example.com/1"},
			{Name: "Doc 2", Url: "https://example.com/2"},
			{Name: "Doc 3", Url: "https://example.com/3"},
		}

		for _, doc := range docs {
			err := store.Create(ctx, doc, threatModelID)
			require.NoError(t, err)
		}

		// Test list all
		retrieved, err := store.List(ctx, threatModelID, 0, 10)
		require.NoError(t, err)
		assert.Len(t, retrieved, 3)

		// Test pagination
		page1, err := store.List(ctx, threatModelID, 0, 2)
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		page2, err := store.List(ctx, threatModelID, 2, 2)
		require.NoError(t, err)
		assert.Len(t, page2, 1)
	})

	t.Run("BulkCreate", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		threatModelID := uuid.New().String()
		docs := []Document{
			{Name: "Bulk 1", Url: "https://example.com/bulk1"},
			{Name: "Bulk 2", Url: "https://example.com/bulk2"},
		}

		err := store.BulkCreate(ctx, docs, threatModelID)
		require.NoError(t, err)

		// Verify all documents were created
		for _, doc := range docs {
			assert.NotNil(t, doc.Id)
			retrieved, err := store.Get(ctx, doc.Id.String())
			require.NoError(t, err)
			assert.Equal(t, doc.Name, retrieved.Name)
		}
	})

	t.Run("Cache operations are no-ops", func(t *testing.T) {
		store := NewInMemoryDocumentStore()
		threatModelID := uuid.New().String()
		err := store.InvalidateCache(ctx, "test-id")
		assert.NoError(t, err)

		err = store.WarmCache(ctx, threatModelID)
		assert.NoError(t, err)
	})
}

// TestInMemorySourceStore tests the in-memory source store implementation
func TestInMemorySourceStore(t *testing.T) {
	ctx := context.Background()

	t.Run("Create and Get", func(t *testing.T) {
		store := NewInMemorySourceStore()
		threatModelID := uuid.New().String()
		name := "Test Source"
		src := &Source{
			Name: &name,
			Url:  "https://github.com/test/repo",
		}

		err := store.Create(ctx, src, threatModelID)
		require.NoError(t, err)
		assert.NotNil(t, src.Id)

		retrieved, err := store.Get(ctx, src.Id.String())
		require.NoError(t, err)
		assert.Equal(t, *src.Name, *retrieved.Name)
		assert.Equal(t, src.Url, retrieved.Url)
		assert.Equal(t, src.Id, retrieved.Id)
	})

	t.Run("Get non-existent source", func(t *testing.T) {
		store := NewInMemorySourceStore()
		_, err := store.Get(ctx, uuid.New().String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Update", func(t *testing.T) {
		store := NewInMemorySourceStore()
		threatModelID := uuid.New().String()
		origName := "Original Source"
		src := &Source{
			Name: &origName,
			Url:  "https://github.com/original/repo",
		}

		err := store.Create(ctx, src, threatModelID)
		require.NoError(t, err)

		updName := "Updated Source"
		src.Name = &updName
		src.Url = "https://github.com/updated/repo"

		err = store.Update(ctx, src, threatModelID)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, src.Id.String())
		require.NoError(t, err)
		assert.Equal(t, "Updated Source", *retrieved.Name)
		assert.Equal(t, "https://github.com/updated/repo", retrieved.Url)
	})

	t.Run("Update non-existent source", func(t *testing.T) {
		store := NewInMemorySourceStore()
		threatModelID := uuid.New().String()
		nonExName := "Non-existent"
		src := &Source{
			Id:   &[]uuid.UUID{uuid.New()}[0],
			Name: &nonExName,
		}

		err := store.Update(ctx, src, threatModelID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Delete", func(t *testing.T) {
		store := NewInMemorySourceStore()
		threatModelID := uuid.New().String()
		delName := "To Delete"
		src := &Source{
			Name: &delName,
			Url:  "https://github.com/delete/repo",
		}

		err := store.Create(ctx, src, threatModelID)
		require.NoError(t, err)

		err = store.Delete(ctx, src.Id.String())
		require.NoError(t, err)

		_, err = store.Get(ctx, src.Id.String())
		assert.Error(t, err)
	})

	t.Run("List sources", func(t *testing.T) {
		store := NewInMemorySourceStore()
		threatModelID := uuid.New().String()
		// Create test sources
		name1 := "Source 1"
		name2 := "Source 2"
		sources := []*Source{
			{Name: &name1, Url: "https://github.com/test/repo1"},
			{Name: &name2, Url: "https://github.com/test/repo2"},
		}

		for _, src := range sources {
			err := store.Create(ctx, src, threatModelID)
			require.NoError(t, err)
		}

		retrieved, err := store.List(ctx, threatModelID, 0, 10)
		require.NoError(t, err)
		assert.Len(t, retrieved, 2)
	})

	t.Run("BulkCreate", func(t *testing.T) {
		store := NewInMemorySourceStore()
		threatModelID := uuid.New().String()
		bulkName1 := "Bulk Source 1"
		bulkName2 := "Bulk Source 2"
		sources := []Source{
			{Name: &bulkName1, Url: "https://github.com/bulk/repo1"},
			{Name: &bulkName2, Url: "https://github.com/bulk/repo2"},
		}

		err := store.BulkCreate(ctx, sources, threatModelID)
		require.NoError(t, err)

		for _, src := range sources {
			assert.NotNil(t, src.Id)
		}
	})

	t.Run("Cache operations are no-ops", func(t *testing.T) {
		store := NewInMemorySourceStore()
		threatModelID := uuid.New().String()
		err := store.InvalidateCache(ctx, "test-id")
		assert.NoError(t, err)

		err = store.WarmCache(ctx, threatModelID)
		assert.NoError(t, err)
	})
}

// TestInMemoryThreatStore tests the in-memory threat store implementation
func TestInMemoryThreatStore(t *testing.T) {
	ctx := context.Background()

	t.Run("Create and Get", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		desc := "Potential SQL injection vulnerability"
		threat := &Threat{
			Name:        "SQL Injection",
			Description: &desc,
		}

		err := store.Create(ctx, threat)
		require.NoError(t, err)
		assert.NotNil(t, threat.Id)

		retrieved, err := store.Get(ctx, threat.Id.String())
		require.NoError(t, err)
		assert.Equal(t, threat.Name, retrieved.Name)
		assert.Equal(t, *threat.Description, *retrieved.Description)
		assert.Equal(t, threat.Id, retrieved.Id)
	})

	t.Run("Get non-existent threat", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		_, err := store.Get(ctx, uuid.New().String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Update", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		origDesc := "Original description"
		threat := &Threat{
			Name:        "Original Threat",
			Description: &origDesc,
		}

		err := store.Create(ctx, threat)
		require.NoError(t, err)

		threat.Name = "Updated Threat"
		updDesc := "Updated description"
		threat.Description = &updDesc

		err = store.Update(ctx, threat)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, threat.Id.String())
		require.NoError(t, err)
		assert.Equal(t, "Updated Threat", retrieved.Name)
		assert.Equal(t, "Updated description", *retrieved.Description)
	})

	t.Run("Update non-existent threat", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		threat := &Threat{
			Id:   &[]uuid.UUID{uuid.New()}[0],
			Name: "Non-existent",
		}

		err := store.Update(ctx, threat)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Delete", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		delDesc := "This threat will be deleted"
		threat := &Threat{
			Name:        "To Delete",
			Description: &delDesc,
		}

		err := store.Create(ctx, threat)
		require.NoError(t, err)

		err = store.Delete(ctx, threat.Id.String())
		require.NoError(t, err)

		_, err = store.Get(ctx, threat.Id.String())
		assert.Error(t, err)
	})

	t.Run("Delete non-existent threat", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		err := store.Delete(ctx, uuid.New().String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("List threats", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		threatModelID := uuid.New().String()

		// Create test threats
		desc1 := "First threat"
		desc2 := "Second threat"
		desc3 := "Third threat"
		threats := []*Threat{
			{Name: "Threat 1", Description: &desc1},
			{Name: "Threat 2", Description: &desc2},
			{Name: "Threat 3", Description: &desc3},
		}

		for _, threat := range threats {
			err := store.Create(ctx, threat)
			require.NoError(t, err)
		}

		// Test list all
		retrieved, err := store.ListSimple(ctx, threatModelID, 0, 10)
		require.NoError(t, err)
		assert.Len(t, retrieved, 3)

		// Test pagination
		page1, err := store.ListSimple(ctx, threatModelID, 0, 2)
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		page2, err := store.ListSimple(ctx, threatModelID, 2, 2)
		require.NoError(t, err)
		assert.Len(t, page2, 1)
	})

	t.Run("Patch - simple implementation", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		patchDesc := "Original description"
		threat := &Threat{
			Name:        "Patch Test",
			Description: &patchDesc,
		}

		err := store.Create(ctx, threat)
		require.NoError(t, err)

		// Test patch operation (currently just returns the existing threat)
		operations := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "Patched Name"},
		}

		patched, err := store.Patch(ctx, threat.Id.String(), operations)
		require.NoError(t, err)
		assert.Equal(t, threat.Name, patched.Name) // Should be unchanged in simple implementation
	})

	t.Run("Patch non-existent threat", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		operations := []PatchOperation{
			{Op: "replace", Path: "/name", Value: "Test"},
		}

		_, err := store.Patch(ctx, uuid.New().String(), operations)
		assert.Error(t, err)
	})

	t.Run("BulkCreate", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		bulkDesc1 := "First bulk threat"
		bulkDesc2 := "Second bulk threat"
		threats := []Threat{
			{Name: "Bulk Threat 1", Description: &bulkDesc1},
			{Name: "Bulk Threat 2", Description: &bulkDesc2},
		}

		err := store.BulkCreate(ctx, threats)
		require.NoError(t, err)

		// Verify all threats were created
		for _, threat := range threats {
			assert.NotNil(t, threat.Id)
			retrieved, err := store.Get(ctx, threat.Id.String())
			require.NoError(t, err)
			assert.Equal(t, threat.Name, retrieved.Name)
		}
	})

	t.Run("BulkUpdate", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		// Create initial threats
		updDesc1 := "Original 1"
		updDesc2 := "Original 2"
		threats := []Threat{
			{Name: "Update Test 1", Description: &updDesc1},
			{Name: "Update Test 2", Description: &updDesc2},
		}

		err := store.BulkCreate(ctx, threats)
		require.NoError(t, err)

		// Modify the threats
		newDesc1 := "Updated 1"
		newDesc2 := "Updated 2"
		threats[0].Description = &newDesc1
		threats[1].Description = &newDesc2

		err = store.BulkUpdate(ctx, threats)
		require.NoError(t, err)

		// Verify updates
		for _, threat := range threats {
			retrieved, err := store.Get(ctx, threat.Id.String())
			require.NoError(t, err)
			assert.Contains(t, *retrieved.Description, "Updated")
		}
	})

	t.Run("Cache operations are no-ops", func(t *testing.T) {
		store := NewInMemoryThreatStore()
		err := store.InvalidateCache(ctx, "test-id")
		assert.NoError(t, err)

		err = store.WarmCache(ctx, "threat-model-id")
		assert.NoError(t, err)
	})
}

// TestThreatModelInMemoryStore tests the in-memory threat model store implementation
func TestThreatModelInMemoryStore(t *testing.T) {
	t.Run("CRUD operations", func(t *testing.T) {
		store := NewThreatModelInMemoryStore()
		desc := "Test description"
		tm := ThreatModel{
			Name:        "Test Threat Model",
			Description: &desc,
		}

		idSetter := func(tm ThreatModel, id string) ThreatModel {
			uuid, _ := uuid.Parse(id)
			tm.Id = &uuid
			return tm
		}

		// Test create
		created, err := store.Create(tm, idSetter)
		require.NoError(t, err)
		assert.NotNil(t, created.Id)
		assert.Equal(t, tm.Name, created.Name)

		// Test get
		retrieved, err := store.Get(created.Id.String())
		require.NoError(t, err)
		assert.Equal(t, created.Id, retrieved.Id)
		assert.Equal(t, created.Name, retrieved.Name)

		// Test list
		items := store.List(0, 10, nil)
		assert.Len(t, items, 1)
		assert.Equal(t, created.Id, items[0].Id)

		// Test count
		assert.Equal(t, 1, store.Count())

		// Test update
		updatedDesc := "Updated description"
		retrieved.Description = &updatedDesc
		err = store.Update(retrieved.Id.String(), retrieved)
		require.NoError(t, err)

		updated, err := store.Get(retrieved.Id.String())
		require.NoError(t, err)
		assert.Equal(t, "Updated description", *updated.Description)

		// Test delete
		err = store.Delete(updated.Id.String())
		require.NoError(t, err)

		_, err = store.Get(updated.Id.String())
		assert.Error(t, err)
		assert.Empty(t, store.List(0, 10, nil))
	})

	t.Run("ListWithCounts", func(t *testing.T) {
		store := NewThreatModelInMemoryStore()
		// Create threat model with embedded entities
		documents := []Document{
			{Name: "Doc 1"},
			{Name: "Doc 2"},
		}
		sourceName := "Source 1"
		sources := []Source{
			{Name: &sourceName},
		}
		// Use simple diagrams for counting
		diagrams := []Diagram{{}, {}, {}}
		threats := []Threat{
			{Name: "Threat 1"},
		}

		tm := ThreatModel{
			Name:       "Count Test Model",
			Documents:  &documents,
			SourceCode: &sources,
			Diagrams:   &diagrams,
			Threats:    &threats,
		}

		idSetter := func(tm ThreatModel, id string) ThreatModel {
			uuid, _ := uuid.Parse(id)
			tm.Id = &uuid
			return tm
		}

		created, err := store.Create(tm, idSetter)
		require.NoError(t, err)

		// Test ListWithCounts
		withCounts := store.ListWithCounts(0, 10, nil)
		require.Len(t, withCounts, 1)

		result := withCounts[0]
		assert.Equal(t, created.Id, result.Id)
		assert.Equal(t, 2, result.DocumentCount)
		assert.Equal(t, 1, result.SourceCount)
		assert.Equal(t, 3, result.DiagramCount)
		assert.Equal(t, 1, result.ThreatCount)
	})

	t.Run("ListWithCounts empty entities", func(t *testing.T) {
		store := NewThreatModelInMemoryStore()
		tm := ThreatModel{
			Name: "Empty Entities Model",
			// No embedded entities
		}

		idSetter := func(tm ThreatModel, id string) ThreatModel {
			uuid, _ := uuid.Parse(id)
			tm.Id = &uuid
			return tm
		}

		created, err := store.Create(tm, idSetter)
		require.NoError(t, err)

		withCounts := store.ListWithCounts(0, 10, nil)
		require.Len(t, withCounts, 1)

		result := withCounts[0]
		assert.Equal(t, created.Id, result.Id)
		assert.Equal(t, 0, result.DocumentCount)
		assert.Equal(t, 0, result.SourceCount)
		assert.Equal(t, 0, result.DiagramCount)
		assert.Equal(t, 0, result.ThreatCount)
	})
}

// TestDiagramInMemoryStore tests the in-memory diagram store implementation
func TestDiagramInMemoryStore(t *testing.T) {
	t.Run("CRUD operations", func(t *testing.T) {
		store := NewDiagramInMemoryStore()
		diagram := DfdDiagram{
			Name:  "Test Diagram",
			Cells: []DfdDiagram_Cells_Item{},
			Type:  "dfd",
		}

		idSetter := func(d DfdDiagram, id string) DfdDiagram {
			uuid, _ := uuid.Parse(id)
			d.Id = &uuid
			return d
		}

		// Test create
		created, err := store.Create(diagram, idSetter)
		require.NoError(t, err)
		assert.NotNil(t, created.Id)
		assert.Equal(t, diagram.Name, created.Name)

		// Test CreateWithThreatModel (should be same as Create for in-memory)
		diagram2 := DfdDiagram{
			Name:  "Test Diagram 2",
			Cells: []DfdDiagram_Cells_Item{},
			Type:  "dfd",
		}
		threatModelID := uuid.New().String()

		created2, err := store.CreateWithThreatModel(diagram2, threatModelID, idSetter)
		require.NoError(t, err)
		assert.NotNil(t, created2.Id)
		assert.Equal(t, diagram2.Name, created2.Name)

		// Test get
		retrieved, err := store.Get(created.Id.String())
		require.NoError(t, err)
		assert.Equal(t, created.Id, retrieved.Id)
		assert.Equal(t, created.Name, retrieved.Name)

		// Test list
		items := store.List(0, 10, nil)
		assert.Len(t, items, 2) // We created 2 diagrams

		// Test count
		assert.Equal(t, 2, store.Count())

		// Test update
		retrieved.Name = "Updated diagram name"
		err = store.Update(retrieved.Id.String(), retrieved)
		require.NoError(t, err)

		updated, err := store.Get(retrieved.Id.String())
		require.NoError(t, err)
		assert.Equal(t, "Updated diagram name", updated.Name)

		// Test delete
		err = store.Delete(updated.Id.String())
		require.NoError(t, err)

		_, err = store.Get(updated.Id.String())
		assert.Error(t, err)
		assert.Equal(t, 1, store.Count()) // Should have 1 remaining
	})
}

// TestParseUUIDOrNil tests the UUID parsing utility function
func TestParseUUIDOrNil(t *testing.T) {
	t.Run("Valid UUID", func(t *testing.T) {
		validUUID := "550e8400-e29b-41d4-a716-446655440000"
		result := ParseUUIDOrNil(validUUID)
		assert.NotEqual(t, uuid.Nil, result)
		assert.Equal(t, validUUID, result.String())
	})

	t.Run("Invalid UUID", func(t *testing.T) {
		invalidUUID := "not-a-uuid"
		result := ParseUUIDOrNil(invalidUUID)
		assert.Equal(t, uuid.Nil, result)
	})

	t.Run("Empty string", func(t *testing.T) {
		result := ParseUUIDOrNil("")
		assert.Equal(t, uuid.Nil, result)
	})
}

// TestInitializeStores tests store initialization functions
func TestInitializeStores(t *testing.T) {
	// NOTE: InitializeInMemoryStores test removed - function no longer exists
	// All stores now use database implementations only
}

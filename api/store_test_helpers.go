package api

// InsertDiagramForTest inserts a diagram with a specific ID directly into the store
// This is only for testing purposes
func InsertDiagramForTest(id string, diagram DfdDiagram) {
	// For database stores, we need to use the regular Create method
	// For in-memory stores, we can access the underlying implementation
	if inMemoryStore, ok := DiagramStore.(*DiagramInMemoryStore); ok {
		inMemoryStore.mutex.Lock()
		defer inMemoryStore.mutex.Unlock()
		inMemoryStore.data[id] = diagram
	} else {
		// For database stores, we use the regular interface
		// This is a test helper limitation when using database stores
		_, _ = DiagramStore.Create(diagram, func(d DfdDiagram, generatedId string) DfdDiagram {
			// Override the generated ID with the test ID
			parsedId, _ := ParseUUID(id)
			d.Id = &parsedId
			return d
		})
	}
}

package api

// InsertDiagramForTest inserts a diagram with a specific ID directly into the store
// This is only for testing purposes
func InsertDiagramForTest(id string, diagram DfdDiagram) {
	// Always use the regular Create method since we only have database stores
	_, _ = DiagramStore.Create(diagram, func(d DfdDiagram, generatedId string) DfdDiagram {
		// Override the generated ID with the test ID
		parsedId, _ := ParseUUID(id)
		d.Id = &parsedId
		return d
	})
}

package api

// InsertDiagramForTest inserts a diagram with a specific ID directly into the store
// This is only for testing purposes
func InsertDiagramForTest(id string, diagram Diagram) {
	DiagramStore.mutex.Lock()
	defer DiagramStore.mutex.Unlock()
	DiagramStore.data[id] = diagram
}

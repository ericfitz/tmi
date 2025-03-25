import { createSelector, createFeatureSelector } from '@ngrx/store';
import { DiagramState } from '../reducers/diagram.reducer';
import { DiagramElement } from '../models/diagram.model';

// Feature selector
export const selectDiagramState = createFeatureSelector<DiagramState>('diagram');

// Basic selectors
export const selectCurrentDiagram = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.currentDiagram
);

export const selectDiagramList = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.diagramList
);

export const selectIsLoading = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.loading
);

export const selectError = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.error
);

// Elements selectors
export const selectDiagramElements = createSelector(
  selectCurrentDiagram,
  (diagram) => diagram?.elements || []
);

// Import specific selector types from NgRx
import { MemoizedSelector } from '@ngrx/store';
import { AppState } from '../../../store';

// Factory function for selecting a diagram element by ID with proper return type
export function selectDiagramElementById(id: string): MemoizedSelector<AppState, DiagramElement | null> {
  return createSelector(
    selectDiagramElements,
    (elements: DiagramElement[]) => elements.find(element => element.id === id) || null
  );
}

export const selectSelectedElementId = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.selectedElementId
);

export const selectSelectedElementIds = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.selectedElementIds
);

export const selectSelectedElement = createSelector(
  selectDiagramElements,
  selectSelectedElementId,
  (elements, selectedId) => selectedId ? elements.find(element => element.id === selectedId) || null : null
);

export const selectSelectedElements = createSelector(
  selectDiagramElements,
  selectSelectedElementIds,
  (elements, selectedIds) => elements.filter(element => selectedIds.includes(element.id))
);

// Undo/Redo stack selectors
export const selectCanUndo = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.undoStack.length > 0
);

export const selectCanRedo = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.redoStack.length > 0
);

// View settings selectors
export const selectZoomLevel = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.zoomLevel
);

export const selectShowGrid = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.showGrid
);

export const selectGridSize = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.gridSize
);

// Compound selectors
export const selectDiagramName = createSelector(
  selectCurrentDiagram,
  (diagram) => diagram?.name || ''
);

export const selectDiagramProperties = createSelector(
  selectCurrentDiagram,
  (diagram) => diagram?.properties || {}
);

export const selectDiagramVersion = createSelector(
  selectCurrentDiagram,
  (diagram) => diagram?.version || 0
);

export const selectDiagramMetadata = createSelector(
  selectCurrentDiagram,
  (diagram) => diagram ? {
    id: diagram.id,
    name: diagram.name,
    createdAt: diagram.createdAt,
    updatedAt: diagram.updatedAt
  } : null
);

export const selectDiagramHasChanges = createSelector(
  selectDiagramState,
  (state: DiagramState) => state.undoStack.length > 0
);

// Performance optimized selector for diagram canvas
export const selectDiagramForCanvas = createSelector(
  selectCurrentDiagram,
  selectZoomLevel,
  selectShowGrid,
  selectGridSize,
  selectSelectedElementIds,
  (diagram, zoomLevel, showGrid, gridSize, selectedElementIds) => ({
    elements: diagram?.elements || [],
    zoomLevel,
    showGrid,
    gridSize,
    selectedElementIds,
    backgroundColor: diagram?.properties?.backgroundColor || '#ffffff'
  })
);
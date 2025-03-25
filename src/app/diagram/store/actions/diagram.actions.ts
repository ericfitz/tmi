import { createAction, props } from '@ngrx/store';
import { Diagram, DiagramElement, DiagramMetadata, DiagramProperties } from '../models/diagram.model';
import { ErrorResponse } from '../../../shared/types/common.types';

// Load diagram actions
export const loadDiagram = createAction(
  '[Diagram] Load Diagram',
  props<{ diagramId: string }>()
);

export const loadDiagramSuccess = createAction(
  '[Diagram] Load Diagram Success',
  props<{ diagram: Diagram }>()
);

export const loadDiagramFailure = createAction(
  '[Diagram] Load Diagram Failure',
  props<{ error: ErrorResponse }>()
);

// Save diagram actions
export const saveDiagram = createAction(
  '[Diagram] Save Diagram'
);

export const saveDiagramSuccess = createAction(
  '[Diagram] Save Diagram Success',
  props<{ diagram: Diagram }>()
);

export const saveDiagramFailure = createAction(
  '[Diagram] Save Diagram Failure',
  props<{ error: ErrorResponse }>()
);

// Create new diagram
export const createDiagram = createAction(
  '[Diagram] Create Diagram',
  props<{ name: string, properties?: DiagramProperties }>()
);

export const createDiagramSuccess = createAction(
  '[Diagram] Create Diagram Success',
  props<{ diagram: Diagram }>()
);

export const createDiagramFailure = createAction(
  '[Diagram] Create Diagram Failure',
  props<{ error: ErrorResponse }>()
);

// Element actions
export const addElement = createAction(
  '[Diagram] Add Element',
  props<{ element: Partial<DiagramElement> }>()
);

export const updateElement = createAction(
  '[Diagram] Update Element',
  props<{ id: string, changes: Partial<DiagramElement> }>()
);

export const removeElement = createAction(
  '[Diagram] Remove Element',
  props<{ id: string }>()
);

export const moveElement = createAction(
  '[Diagram] Move Element',
  props<{ id: string, position: { x: number, y: number } }>()
);

export const resizeElement = createAction(
  '[Diagram] Resize Element',
  props<{ id: string, size: { width: number, height: number } }>()
);

export const selectElement = createAction(
  '[Diagram] Select Element',
  props<{ id: string | null }>()
);

export const selectMultipleElements = createAction(
  '[Diagram] Select Multiple Elements',
  props<{ ids: string[] }>()
);

// Diagram list actions
export const loadDiagramList = createAction(
  '[Diagram] Load Diagram List'
);

export const loadDiagramListSuccess = createAction(
  '[Diagram] Load Diagram List Success',
  props<{ diagrams: DiagramMetadata[] }>()
);

export const loadDiagramListFailure = createAction(
  '[Diagram] Load Diagram List Failure',
  props<{ error: ErrorResponse }>()
);

// Undo/Redo actions
export const undoAction = createAction(
  '[Diagram] Undo Action'
);

export const redoAction = createAction(
  '[Diagram] Redo Action'
);

// Zoom actions
export const setZoomLevel = createAction(
  '[Diagram] Set Zoom Level',
  props<{ zoomLevel: number }>()
);

// Grid actions
export const toggleGrid = createAction(
  '[Diagram] Toggle Grid',
  props<{ show: boolean }>()
);

export const setGridSize = createAction(
  '[Diagram] Set Grid Size',
  props<{ size: number }>()
);

// Clear the current diagram
export const clearCurrentDiagram = createAction(
  '[Diagram] Clear Current Diagram'
);

// Update diagram properties
export const updateDiagramProperties = createAction(
  '[Diagram] Update Diagram Properties',
  props<{ properties: DiagramProperties }>()
);
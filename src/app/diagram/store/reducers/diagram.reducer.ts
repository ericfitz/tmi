import { createReducer, on } from '@ngrx/store';
import { createEntityAdapter, EntityAdapter } from '@ngrx/entity';
import { Diagram, DiagramElement, DiagramMetadata, DiagramElementType } from '../models/diagram.model';
import * as DiagramActions from '../actions/diagram.actions';
import { ErrorResponse } from '../../../shared/types/common.types';

export interface DiagramState {
  currentDiagram: Diagram | null;
  selectedElementIds: string[];
  diagramList: DiagramMetadata[];
  undoStack: Diagram[];
  redoStack: Diagram[];
  zoomLevel: number;
  showGrid: boolean;
  gridSize: number;
  loading: boolean;
  error: ErrorResponse | null;
  selectedElementId: string | null;
}

// Entity adapter for diagram elements
export const diagramElementsAdapter: EntityAdapter<DiagramElement> = createEntityAdapter<DiagramElement>({
  selectId: (element: DiagramElement) => element.id,
  sortComparer: (a, b) => a.zIndex - b.zIndex
});

export const initialState: DiagramState = {
  currentDiagram: null,
  selectedElementIds: [],
  diagramList: [],
  undoStack: [],
  redoStack: [],
  zoomLevel: 1,
  showGrid: true,
  gridSize: 20,
  loading: false,
  error: null,
  selectedElementId: null
};

export const diagramReducer = createReducer(
  initialState,
  
  // Load diagram
  on(DiagramActions.loadDiagram, (state) => ({
    ...state,
    loading: true,
    error: null
  })),
  
  on(DiagramActions.loadDiagramSuccess, (state, { diagram }) => ({
    ...state,
    currentDiagram: diagram,
    loading: false,
    error: null,
    // Clear undo/redo stacks when loading a new diagram
    undoStack: [],
    redoStack: []
  })),
  
  on(DiagramActions.loadDiagramFailure, (state, { error }) => ({
    ...state,
    loading: false,
    error
  })),
  
  // Save diagram
  on(DiagramActions.saveDiagram, (state) => ({
    ...state,
    loading: true,
    error: null
  })),
  
  on(DiagramActions.saveDiagramSuccess, (state, { diagram }) => ({
    ...state,
    currentDiagram: diagram,
    loading: false,
    error: null
  })),
  
  on(DiagramActions.saveDiagramFailure, (state, { error }) => ({
    ...state,
    loading: false,
    error
  })),
  
  // Create diagram
  on(DiagramActions.createDiagram, (state) => ({
    ...state,
    loading: true,
    error: null
  })),
  
  on(DiagramActions.createDiagramSuccess, (state, { diagram }) => ({
    ...state,
    currentDiagram: diagram,
    loading: false,
    error: null,
    // Clear undo/redo stacks when creating a new diagram
    undoStack: [],
    redoStack: []
  })),
  
  on(DiagramActions.createDiagramFailure, (state, { error }) => ({
    ...state,
    loading: false,
    error
  })),
  
  // Element actions
  on(DiagramActions.addElement, (state, { element }) => {
    if (!state.currentDiagram) return state;
    
    // Create a snapshot for the undo stack
    const undoSnapshot = { ...state.currentDiagram };
    
    // Create the new element with defaults for missing properties
    const newElement = {
      id: element.id || generateUuid(),
      type: element.type!,
      position: element.position || { x: 0, y: 0 },
      size: element.size || { width: 100, height: 100 },
      properties: element.properties || {},
      zIndex: element.zIndex || (state.currentDiagram.elements.length > 0 
        ? Math.max(...state.currentDiagram.elements.map(e => e.zIndex)) + 1 
        : 1)
    } as DiagramElement;
    
    // Add the element to the current diagram
    return {
      ...state,
      currentDiagram: {
        ...state.currentDiagram,
        elements: [...state.currentDiagram.elements, newElement],
        version: state.currentDiagram.version + 1,
        updatedAt: new Date().toISOString()
      },
      undoStack: [undoSnapshot, ...state.undoStack],
      redoStack: [] // Clear redo stack on new action
    };
  }),
  
  on(DiagramActions.updateElement, (state, { id, changes }) => {
    if (!state.currentDiagram) return state;
    
    // Create a snapshot for the undo stack
    const undoSnapshot = { ...state.currentDiagram };
    
    // Find and update the element
    const updatedElements = state.currentDiagram.elements.map(element => 
      element.id === id ? { ...element, ...changes } : element
    );
    
    if (updatedElements.every(e => e.id !== id)) {
      // Element not found, don't modify state
      return state;
    }
    
    return {
      ...state,
      currentDiagram: {
        ...state.currentDiagram,
        elements: updatedElements,
        version: state.currentDiagram.version + 1,
        updatedAt: new Date().toISOString()
      },
      undoStack: [undoSnapshot, ...state.undoStack],
      redoStack: [] // Clear redo stack on new action
    };
  }),
  
  on(DiagramActions.removeElement, (state, { id }) => {
    if (!state.currentDiagram) return state;
    
    // Create a snapshot for the undo stack
    const undoSnapshot = {
      ...state.currentDiagram,
      elements: [...state.currentDiagram.elements]
    };
    
    // Filter out the element to remove
    const filteredElements = state.currentDiagram.elements.filter(element => element.id !== id);
    
    if (filteredElements.length === state.currentDiagram.elements.length) {
      // Element not found, don't modify state
      return state;
    }
    
    // Also remove any connectors that reference this element
    const connectedElementIds = new Set<string>();
    connectedElementIds.add(id);
    
    // Find connectors that reference the deleted element
    state.currentDiagram.elements.forEach(element => {
      if (element.type === DiagramElementType.CONNECTOR || element.type === DiagramElementType.LINE) {
        if (element.properties.sourceElementId === id || element.properties.targetElementId === id) {
          connectedElementIds.add(element.id);
        }
      }
    });
    
    // Remove all related elements
    const fullyFilteredElements = state.currentDiagram.elements.filter(
      element => !connectedElementIds.has(element.id)
    );
    
    return {
      ...state,
      currentDiagram: {
        ...state.currentDiagram,
        elements: fullyFilteredElements,
        version: state.currentDiagram.version + 1,
        updatedAt: new Date().toISOString()
      },
      selectedElementId: state.selectedElementId === id ? null : 
                        (connectedElementIds.has(state.selectedElementId || '') ? null : state.selectedElementId),
      selectedElementIds: state.selectedElementIds.filter(elementId => !connectedElementIds.has(elementId)),
      undoStack: [undoSnapshot, ...state.undoStack],
      redoStack: [] // Clear redo stack on new action
    };
  }),
  
  on(DiagramActions.moveElement, (state, { id, position }) => {
    if (!state.currentDiagram) return state;
    
    // Create a snapshot for the undo stack
    const undoSnapshot = { ...state.currentDiagram };
    
    // Find and update the element position
    const updatedElements = state.currentDiagram.elements.map(element => 
      element.id === id ? { ...element, position } : element
    );
    
    if (updatedElements.every(e => e.id !== id || (e.position.x === position.x && e.position.y === position.y))) {
      // Element not found or position is the same, don't modify state
      return state;
    }
    
    return {
      ...state,
      currentDiagram: {
        ...state.currentDiagram,
        elements: updatedElements,
        version: state.currentDiagram.version + 1,
        updatedAt: new Date().toISOString()
      },
      undoStack: [undoSnapshot, ...state.undoStack],
      redoStack: [] // Clear redo stack on new action
    };
  }),
  
  on(DiagramActions.resizeElement, (state, { id, size }) => {
    if (!state.currentDiagram) return state;
    
    // Create a snapshot for the undo stack
    const undoSnapshot = { ...state.currentDiagram };
    
    // Find and update the element size
    const updatedElements = state.currentDiagram.elements.map(element => 
      element.id === id ? { ...element, size } : element
    );
    
    if (updatedElements.every(e => e.id !== id || (e.size.width === size.width && e.size.height === size.height))) {
      // Element not found or size is the same, don't modify state
      return state;
    }
    
    return {
      ...state,
      currentDiagram: {
        ...state.currentDiagram,
        elements: updatedElements,
        version: state.currentDiagram.version + 1,
        updatedAt: new Date().toISOString()
      },
      undoStack: [undoSnapshot, ...state.undoStack],
      redoStack: [] // Clear redo stack on new action
    };
  }),
  
  // Selection actions
  on(DiagramActions.selectElement, (state, { id }) => ({
    ...state,
    selectedElementId: id,
    selectedElementIds: id ? [id] : []
  })),
  
  on(DiagramActions.selectMultipleElements, (state, { ids }) => ({
    ...state,
    selectedElementId: ids.length === 1 ? ids[0] : null,
    selectedElementIds: ids
  })),
  
  // Diagram list actions
  on(DiagramActions.loadDiagramList, (state) => ({
    ...state,
    loading: true,
    error: null
  })),
  
  on(DiagramActions.loadDiagramListSuccess, (state, { diagrams }) => ({
    ...state,
    diagramList: diagrams,
    loading: false,
    error: null
  })),
  
  on(DiagramActions.loadDiagramListFailure, (state, { error }) => ({
    ...state,
    loading: false,
    error
  })),
  
  // Undo/Redo actions
  on(DiagramActions.undoAction, (state) => {
    if (state.undoStack.length === 0 || !state.currentDiagram) return state;
    
    const [lastState, ...remainingUndoStack] = state.undoStack;
    
    return {
      ...state,
      currentDiagram: lastState,
      undoStack: remainingUndoStack,
      redoStack: [state.currentDiagram, ...state.redoStack]
    };
  }),
  
  on(DiagramActions.redoAction, (state) => {
    if (state.redoStack.length === 0 || !state.currentDiagram) return state;
    
    const [nextState, ...remainingRedoStack] = state.redoStack;
    
    return {
      ...state,
      currentDiagram: nextState,
      redoStack: remainingRedoStack,
      undoStack: [state.currentDiagram, ...state.undoStack]
    };
  }),
  
  // View settings
  on(DiagramActions.setZoomLevel, (state, { zoomLevel }) => ({
    ...state,
    zoomLevel
  })),
  
  on(DiagramActions.toggleGrid, (state, { show }) => ({
    ...state,
    showGrid: show
  })),
  
  on(DiagramActions.setGridSize, (state, { size }) => ({
    ...state,
    gridSize: size
  })),
  
  // Clear diagram
  on(DiagramActions.clearCurrentDiagram, (state) => ({
    ...state,
    currentDiagram: null,
    selectedElementId: null,
    selectedElementIds: [],
    undoStack: [],
    redoStack: []
  })),
  
  // Update diagram properties
  on(DiagramActions.updateDiagramProperties, (state, { properties }) => {
    if (!state.currentDiagram) return state;
    
    // Create a snapshot for the undo stack
    const undoSnapshot = { ...state.currentDiagram };
    
    return {
      ...state,
      currentDiagram: {
        ...state.currentDiagram,
        properties: {
          ...state.currentDiagram.properties,
          ...properties
        },
        version: state.currentDiagram.version + 1,
        updatedAt: new Date().toISOString()
      },
      undoStack: [undoSnapshot, ...state.undoStack],
      redoStack: []
    };
  })
);

// Helper function to generate UUID
function generateUuid(): string {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
    const r = Math.random() * 16 | 0;
    const v = c === 'x' ? r : (r & 0x3 | 0x8);
    return v.toString(16);
  });
}
import { diagramReducer, initialState } from './diagram.reducer';
import * as DiagramActions from '../actions/diagram.actions';
import { DiagramElementType } from '../models/diagram.model';

describe('Diagram Reducer', () => {
  describe('an unknown action', () => {
    it('should return the previous state', () => {
      const action = { type: 'Unknown' };
      const result = diagramReducer(initialState, action);
      
      expect(result).toBe(initialState);
    });
  });
  
  describe('loadDiagram actions', () => {
    it('should set loading to true on loadDiagram', () => {
      const action = DiagramActions.loadDiagram({ diagramId: '123' });
      const result = diagramReducer(initialState, action);
      
      expect(result.loading).toBe(true);
      expect(result.error).toBeNull();
    });
    
    it('should set diagram on loadDiagramSuccess', () => {
      const diagram = {
        id: '123',
        name: 'Test Diagram',
        elements: [],
        properties: { backgroundColor: '#ffffff' },
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: 1
      };
      
      const action = DiagramActions.loadDiagramSuccess({ diagram });
      const result = diagramReducer({...initialState, loading: true}, action);
      
      expect(result.loading).toBe(false);
      expect(result.currentDiagram).toEqual(diagram);
      expect(result.error).toBeNull();
      expect(result.undoStack).toEqual([]);
      expect(result.redoStack).toEqual([]);
    });
    
    it('should set error on loadDiagramFailure', () => {
      const error = { code: 'NOT_FOUND', message: 'Diagram not found' };
      const action = DiagramActions.loadDiagramFailure({ error });
      const result = diagramReducer({...initialState, loading: true}, action);
      
      expect(result.loading).toBe(false);
      expect(result.error).toEqual(error);
    });
  });
  
  describe('element actions', () => {
    it('should add a new element', () => {
      // Setup initial state with a diagram
      const currentDiagram = {
        id: '123',
        name: 'Test Diagram',
        elements: [],
        properties: { backgroundColor: '#ffffff' },
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: 1
      };
      
      const stateWithDiagram = {
        ...initialState,
        currentDiagram
      };
      
      // Create an element to add
      const element = {
        type: DiagramElementType.RECTANGLE,
        position: { x: 100, y: 100 },
        size: { width: 120, height: 60 },
        properties: {
          text: 'Test Rectangle',
          backgroundColor: '#ffffff',
          borderColor: '#000000'
        }
      };
      
      const action = DiagramActions.addElement({ element });
      const result = diagramReducer(stateWithDiagram, action);
      
      // Check that the element was added
      expect(result.currentDiagram?.elements.length).toBe(1);
      expect(result.currentDiagram?.elements[0].type).toBe(DiagramElementType.RECTANGLE);
      expect(result.currentDiagram?.elements[0].position).toEqual({ x: 100, y: 100 });
      expect(result.currentDiagram?.version).toBe(2); // Version should be incremented
      
      // Check that undo stack was updated
      expect(result.undoStack.length).toBe(1);
      expect(result.undoStack[0]).toEqual(currentDiagram);
      
      // Redo stack should be cleared
      expect(result.redoStack.length).toBe(0);
    });
    
    it('should handle element removal', () => {
      // Setup initial state with a diagram that has an element
      const elementId = '123-element';
      const currentDiagram = {
        id: '123',
        name: 'Test Diagram',
        elements: [{
          id: elementId,
          type: DiagramElementType.RECTANGLE,
          position: { x: 100, y: 100 },
          size: { width: 120, height: 60 },
          properties: {
            text: 'Test Rectangle'
          },
          zIndex: 1
        }],
        properties: { backgroundColor: '#ffffff' },
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: 1
      };
      
      const stateWithElement = {
        ...initialState,
        currentDiagram,
        selectedElementId: elementId,
        selectedElementIds: [elementId]
      };
      
      const action = DiagramActions.removeElement({ id: elementId });
      const result = diagramReducer(stateWithElement, action);
      
      // Check that the element was removed
      expect(result.currentDiagram?.elements.length).toBe(0);
      expect(result.currentDiagram?.version).toBe(2); // Version should be incremented
      
      // Selection should be cleared
      expect(result.selectedElementId).toBeNull();
      expect(result.selectedElementIds.length).toBe(0);
      
      // Check that undo stack was updated
      expect(result.undoStack.length).toBe(1);
      expect(result.undoStack[0]).toEqual(currentDiagram);
    });
  });
  
  describe('undo/redo actions', () => {
    it('should restore previous state on undo', () => {
      // Create a state with a diagram and something in the undo stack
      const previousDiagram = {
        id: '123',
        name: 'Previous Version',
        elements: [],
        properties: { backgroundColor: '#eeeeee' },
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: 1
      };
      
      const currentDiagram = {
        ...previousDiagram,
        name: 'Current Version',
        properties: { backgroundColor: '#ffffff' },
        version: 2
      };
      
      const stateWithHistory = {
        ...initialState,
        currentDiagram,
        undoStack: [previousDiagram]
      };
      
      const action = DiagramActions.undoAction();
      const result = diagramReducer(stateWithHistory, action);
      
      // Current diagram should be the previous one from undo stack
      expect(result.currentDiagram).toEqual(previousDiagram);
      
      // Undo stack should be empty now
      expect(result.undoStack.length).toBe(0);
      
      // Redo stack should have the current diagram
      expect(result.redoStack.length).toBe(1);
      expect(result.redoStack[0]).toEqual(currentDiagram);
    });
    
    it('should restore next state on redo', () => {
      // Create a state with a diagram and something in the redo stack
      const currentDiagram = {
        id: '123',
        name: 'Current Version',
        elements: [],
        properties: { backgroundColor: '#eeeeee' },
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: 1
      };
      
      const nextDiagram = {
        ...currentDiagram,
        name: 'Next Version',
        properties: { backgroundColor: '#ffffff' },
        version: 2
      };
      
      const stateWithRedoHistory = {
        ...initialState,
        currentDiagram,
        redoStack: [nextDiagram]
      };
      
      const action = DiagramActions.redoAction();
      const result = diagramReducer(stateWithRedoHistory, action);
      
      // Current diagram should be the next one from redo stack
      expect(result.currentDiagram).toEqual(nextDiagram);
      
      // Redo stack should be empty now
      expect(result.redoStack.length).toBe(0);
      
      // Undo stack should have the current diagram
      expect(result.undoStack.length).toBe(1);
      expect(result.undoStack[0]).toEqual(currentDiagram);
    });
  });
});
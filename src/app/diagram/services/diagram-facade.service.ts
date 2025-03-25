import { Injectable } from '@angular/core';
import { Store } from '@ngrx/store';
import { Observable } from 'rxjs';
import { take } from 'rxjs/operators';
import * as DiagramActions from '../store/actions/diagram.actions';
import { DiagramElement, DiagramElementType, DiagramProperties, Position, Size, Diagram, DiagramMetadata, DiagramElementProperties } from '../store/models/diagram.model';
import { 
  selectCurrentDiagram, 
  selectIsLoading, 
  selectError, 
  selectDiagramList, 
  selectDiagramElementById,
  selectDiagramElements,
  selectSelectedElementId,
  selectCanUndo,
  selectCanRedo,
  selectDiagramHasChanges,
  selectDiagramForCanvas
} from '../store/selectors/diagram.selectors';
import { DiagramState } from '../store/reducers/diagram.reducer';
import { DiagramService, DiagramData, DiagramCell } from './diagram.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { StorageService } from '../../shared/services/storage/storage.service';
import { ErrorResponse } from '../../shared/types/common.types';
import { StorageFile } from '../../shared/services/storage/providers/storage-provider.interface';

@Injectable({
  providedIn: 'root'
})
export class DiagramFacadeService {
  // Selectors as Observables
  currentDiagram$: Observable<Diagram | null>;
  isLoading$: Observable<boolean>;
  error$: Observable<ErrorResponse | null>;
  diagramList$: Observable<DiagramMetadata[]>;
  allElements$: Observable<DiagramElement[]>;
  selectedElementId$: Observable<string | null>;
  canUndo$: Observable<boolean>;
  canRedo$: Observable<boolean>;
  diagramForCanvas$: Observable<{
    elements: DiagramElement[];
    zoomLevel: number;
    showGrid: boolean;
    gridSize: number;
    selectedElementIds: string[];
    backgroundColor: string;
  }>;
  hasChanges$: Observable<boolean>;

  constructor(
    private store: Store<{ diagram: DiagramState }>,
    private diagramService: DiagramService,
    private storageService: StorageService,
    private logger: LoggerService
  ) {
    this.currentDiagram$ = this.store.select(selectCurrentDiagram);
    this.isLoading$ = this.store.select(selectIsLoading);
    this.error$ = this.store.select(selectError);
    this.diagramList$ = this.store.select(selectDiagramList);
    this.allElements$ = this.store.select(selectDiagramElements);
    this.selectedElementId$ = this.store.select(selectSelectedElementId);
    this.canUndo$ = this.store.select(selectCanUndo);
    this.canRedo$ = this.store.select(selectCanRedo);
    this.diagramForCanvas$ = this.store.select(selectDiagramForCanvas);
    this.hasChanges$ = this.store.select(selectDiagramHasChanges);
    
    // Initialize storage early
    this.initializeStorage();
  }

  /**
   * Initialize storage provider asynchronously
   */
  private async initializeStorage(): Promise<void> {
    try {
      await this.storageService.initialize();
      this.logger.debug('Storage initialized', 'DiagramFacadeService');
    } catch (error) {
      this.logger.warn('Failed to initialize storage provider', 'DiagramFacadeService', error);
    }
  }

  // CRUD operations for diagrams
  
  /**
   * Create a new blank diagram
   * @returns Promise with result object indicating success/failure
   */
  async createDiagram(name: string, properties?: DiagramProperties): Promise<{ success: boolean; error?: Error }> {
    this.logger.debug(`Creating new diagram: ${name}`, 'DiagramFacadeService');
    
    this.store.dispatch(DiagramActions.createDiagram({ name, properties }));
    
    try {
      // Make sure storage is initialized before attempting to reset diagram
      await this.storageService.initialize().catch(err => {
        this.logger.warn('Storage initialization error during diagram creation', 'DiagramFacadeService', err);
        // Continue anyway as we might be able to create an in-memory diagram
      });
      
      // Create a new blank diagram manually if resetDiagram fails
      let blankDiagram: DiagramData;
      
      try {
        // First try using the diagram service's method
        this.diagramService.resetDiagram();
        const existingDiagram = this.diagramService.getCurrentDiagram();
        
        if (existingDiagram) {
          blankDiagram = existingDiagram;
        } else {
          throw new Error('DiagramService did not return a diagram');
        }
      } catch (resetError) {
        this.logger.warn('Failed to reset diagram via service, creating manually', 'DiagramFacadeService', resetError);
        
        // Create a basic diagram structure manually if the service method fails
        blankDiagram = {
          cells: [],
          title: name,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
          version: '1.0',
          properties: properties as Record<string, unknown>
        };
      }
      
      // Update the name and properties
      blankDiagram.title = name;
      
      // Apply optional properties if provided
      if (properties) {
        blankDiagram.properties = properties;
      }
      
      // Convert to diagram model and dispatch success action
      const newDiagram = this.convertToDiagramModel(blankDiagram);
      
      if (!newDiagram) {
        throw new Error('Failed to convert diagram model');
      }
      
      this.store.dispatch(DiagramActions.createDiagramSuccess({ diagram: newDiagram as Diagram }));
      return { success: true };
    } catch (error) {
      this.logger.error('Failed to create diagram', 'DiagramFacadeService', error);
      this.store.dispatch(DiagramActions.createDiagramFailure({ 
        error: error instanceof Error ? error : new Error('Failed to create blank diagram') 
      }));
      return { 
        success: false, 
        error: error instanceof Error ? error : new Error('Failed to create blank diagram') 
      };
    }
  }
  
  /**
   * Load diagram from storage by ID
   */
  async loadDiagram(diagramId: string): Promise<void> {
    this.logger.debug(`Loading diagram: ${diagramId}`, 'DiagramFacadeService');
    
    try {
      this.store.dispatch(DiagramActions.loadDiagram({ diagramId }));
      
      // Make sure storage is initialized
      try {
        await this.storageService.initialize();
      } catch (initError) {
        this.logger.warn('Storage initialization failed', 'DiagramFacadeService', initError);
        // Create a new blank diagram instead
        this.createDiagram('New Diagram');
        return;
      }
      
      // Handle empty ID by creating a new diagram
      if (!diagramId) {
        this.logger.info('No diagram ID provided, creating new diagram', 'DiagramFacadeService');
        this.createDiagram('New Diagram');
        return;
      }
      
      try {
        await this.diagramService.loadDiagram(diagramId);
        const loadedDiagram = this.diagramService.getCurrentDiagram();
        
        if (!loadedDiagram) {
          throw new Error('Failed to load diagram, data not found');
        }
        
        const diagram = this.convertToDiagramModel(loadedDiagram);
        this.store.dispatch(DiagramActions.loadDiagramSuccess({ diagram: diagram as Diagram }));
      } catch (loadError) {
        // Handle specific load error by returning a failure
        this.logger.error(`Error loading diagram: ${diagramId}`, 'DiagramFacadeService', loadError);
        this.store.dispatch(DiagramActions.loadDiagramFailure({ 
          error: {
            message: loadError instanceof Error ? loadError.message : 'Failed to load diagram',
            details: { diagramId }
          } 
        }));
        
        // Fallback to a new diagram
        this.createDiagram('New Diagram');
      }
    } catch (error) {
      // Handle any other unforeseen errors
      this.logger.error('Unexpected error loading diagram', 'DiagramFacadeService', error);
      this.store.dispatch(DiagramActions.loadDiagramFailure({ 
        error: {
          message: error instanceof Error ? error.message : 'Unknown error loading diagram',
          details: { errorMessage: error instanceof Error ? error.message : 'Unknown error', diagramId }
        } 
      }));
    }
  }
  
  /**
   * Save the current diagram
   */
  async saveDiagram(): Promise<void> {
    this.logger.debug('Saving diagram', 'DiagramFacadeService');
    
    try {
      // Completely different approach - use a variable with direct value assignment
      const diagramName = 'Untitled Diagram';
      
      // Notify the store we're beginning the save operation
      this.store.dispatch(DiagramActions.saveDiagram());
      
      // Save using a default name - the diagram service should handle the actual diagram data
      await this.diagramService.saveDiagram(diagramName);
      
      const updatedDiagram = this.convertToDiagramModel(this.diagramService.getCurrentDiagram());
      this.store.dispatch(DiagramActions.saveDiagramSuccess({ diagram: updatedDiagram as Diagram }));
    } catch (error) {
      this.logger.error('Error saving diagram', 'DiagramFacadeService', error);
      this.store.dispatch(DiagramActions.saveDiagramFailure({ 
        error: {
          message: error instanceof Error ? error.message : 'Error saving diagram',
          details: { errorMessage: error instanceof Error ? error.message : 'Unknown error' }
        } 
      }));
    }
  }
  
  /**
   * Load the list of diagrams from storage
   */
  async loadDiagramList(): Promise<void> {
    this.logger.debug('Loading diagram list', 'DiagramFacadeService');
    this.store.dispatch(DiagramActions.loadDiagramList());
    
    try {
      // Initialize the storage provider first
      try {
        await this.storageService.initialize();
      } catch (initError) {
        this.logger.warn('Storage initialization failed, creating new diagram', 'DiagramFacadeService', initError);
        // If storage initialization fails, just use an empty list
        this.store.dispatch(DiagramActions.loadDiagramListSuccess({ diagrams: [] }));
        return;
      }
      
      // Try to initialize current file if needed
      try {
        if (!this.diagramService.getCurrentFile()) {
          await this.diagramService.resetDiagram();
        }
      } catch (fileError) {
        this.logger.warn('Failed to initialize diagram file', 'DiagramFacadeService', fileError);
        // Continue execution to at least try to get the list
      }
      
      try {
        const files = await this.diagramService.loadDiagramList();
        const diagrams = this.mapFilesToDiagramMetadata(files);
        this.store.dispatch(DiagramActions.loadDiagramListSuccess({ diagrams }));
      } catch (listError) {
        this.logger.error('Failed to load diagram list', 'DiagramFacadeService', listError);
        // If listing fails, return an empty list instead of an error
        this.store.dispatch(DiagramActions.loadDiagramListSuccess({ diagrams: [] }));
      }
    } catch (error) {
      // This is our fallback for any other unforeseen errors
      this.logger.error('Unexpected error loading diagram list', 'DiagramFacadeService', error);
      this.store.dispatch(DiagramActions.loadDiagramListFailure({ 
        error: {
          message: error instanceof Error ? error.message : 'Error loading diagram list',
          details: { errorMessage: error instanceof Error ? error.message : 'Unknown error' }
        } 
      }));
    }
  }
  
  // CRUD operations for diagram elements
  
  /**
   * Add a new element to the diagram
   */
  addElement(type: DiagramElementType, position: Position, size: Size, properties?: DiagramElementProperties): void {
    this.logger.info(`Adding element of type: ${type}`, 'DiagramFacadeService');
    
    // Generate a unique ID for the element
    const elementId = this.generateUuid();
    
    // Default properties based on element type
    let defaultProperties: DiagramElementProperties = {
      text: type.toString()
    };
    
    // Set specific default properties for different types
    if (type === DiagramElementType.CIRCLE) {
      defaultProperties = {
        ...defaultProperties,
        backgroundColor: '#f0f0f0',
        borderColor: '#000000',
        shapeType: 'circle'
      };
      
      // For circles, ensure width and height are equal
      size = {
        width: Math.max(size.width, size.height),
        height: Math.max(size.width, size.height)
      };
    } else if (type === DiagramElementType.RECTANGLE) {
      defaultProperties = {
        ...defaultProperties,
        backgroundColor: '#ffffff',
        borderColor: '#000000'
      };
    } else if (type === DiagramElementType.TEXT) {
      defaultProperties = {
        ...defaultProperties,
        color: '#000000',
        fontSize: 14,
        fontFamily: 'Arial'
      };
    }
    
    // Create the element with proper properties
    const element = {
      id: elementId,
      type,
      position,
      size,
      properties: {
        ...defaultProperties,
        ...properties
      }
    };
    
    // Log for debugging
    this.logger.debug(`Element details: ${JSON.stringify({
      id: element.id,
      type: element.type,
      position: element.position,
      size: element.size,
      properties: element.properties
    })}`, 'DiagramFacadeService');
    
    // Dispatch the action to add the element
    this.store.dispatch(DiagramActions.addElement({ element }));
  }
  
  /**
   * Remove an element from the diagram by ID
   */
  removeElement(id: string): void {
    this.store.dispatch(DiagramActions.removeElement({ id }));
  }
  
  /**
   * Update an element's properties
   */
  updateElement(id: string, changes: Partial<DiagramElement>): void {
    this.store.dispatch(DiagramActions.updateElement({ id, changes }));
  }
  
  /**
   * Select an element by ID
   */
  selectElement(id: string | null): void {
    this.store.dispatch(DiagramActions.selectElement({ id }));
  }
  
  /**
   * Get an element by ID
   */
  getElement(id: string): Observable<DiagramElement | null> {
    return this.store.select(selectDiagramElementById(id));
  }
  
  // History operations
  
  /**
   * Undo the last action
   */
  undo(): void {
    this.store.dispatch(DiagramActions.undoAction());
  }
  
  /**
   * Redo the last undone action
   */
  redo(): void {
    this.store.dispatch(DiagramActions.redoAction());
  }
  
  // UI operations
  
  /**
   * Toggle grid visibility
   */
  toggleGrid(show: boolean): void {
    this.store.dispatch(DiagramActions.toggleGrid({ show }));
  }
  
  /**
   * Clear the current diagram
   */
  clearDiagram(): void {
    this.store.dispatch(DiagramActions.clearCurrentDiagram());
  }
  
  // Private helper methods originally from effects
  
  /**
   * Map storage files to diagram metadata format
   */
  private mapFilesToDiagramMetadata(files: StorageFile[]): DiagramMetadata[] {
    return files.map(item => ({
      id: item.id,
      name: item.name,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      lastOpenedAt: new Date().toISOString()
    }));
  }
  
  /**
   * Helper method to convert from DiagramService model to state model
   */
  private convertToDiagramModel(diagramData: DiagramData | null): DiagramState['currentDiagram'] {
    if (!diagramData) {
      this.logger.warn('Attempting to convert null diagram data', 'DiagramFacadeService');
      return null;
    }

    try {
      // Map diagram data to NgRx state model
      const diagram = {
        id: diagramData.id || this.generateUuid(),
        name: diagramData.title || 'Untitled Diagram',
        elements: this.mapCellsToElements(diagramData.cells || []),
        createdAt: diagramData.createdAt || new Date().toISOString(),
        updatedAt: diagramData.updatedAt || new Date().toISOString(),
        version: diagramData.version ? Number(diagramData.version) : 1,
        properties: {
          backgroundColor: '#ffffff',
          gridSize: 20,
          snapToGrid: true,
          ...(diagramData.properties as Record<string, unknown> || {})
        }
      };
      
      // If there's a selected cell ID, select it in our store
      if (diagramData.selectedCellId) {
        this.logger.info(`Setting selected element ID: ${diagramData.selectedCellId}`, 'DiagramFacadeService');
        this.selectElement(diagramData.selectedCellId);
      }
      
      return diagram;
    } catch (error) {
      this.logger.error('Error converting diagram model', 'DiagramFacadeService', error);
      
      // Return a basic diagram in case of error
      return {
        id: this.generateUuid(),
        name: 'Untitled Diagram',
        elements: [],
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: 1,
        properties: {
          backgroundColor: '#ffffff',
          gridSize: 20,
          snapToGrid: true
        }
      };
    }
  }

  /**
   * Helper method to map DiagramService cells to diagram elements
   */
  private mapCellsToElements(cells: DiagramCell[]): DiagramElement[] {
    const elements: DiagramElement[] = [];
    
    cells.forEach(cell => {
      if (cell.isVertex) {
        elements.push({
          id: cell.id,
          type: this.mapCellStyleToType(cell.style || ''),
          position: {
            x: cell.geometry.x,
            y: cell.geometry.y
          },
          size: {
            width: cell.geometry.width,
            height: cell.geometry.height
          },
          properties: {
            text: cell.value,
            ...this.parseStyleProperties(cell.style || '')
          },
          zIndex: 0 // Default z-index
        });
      } else if (cell.isEdge) {
        elements.push({
          id: cell.id,
          type: DiagramElementType.CONNECTOR,
          position: { x: 0, y: 0 }, // Edges don't have a position
          size: { width: 0, height: 0 }, // Edges don't have a size
          properties: {
            text: cell.value,
            sourceElementId: cell.sourceId || undefined,
            targetElementId: cell.targetId || undefined,
            ...this.parseStyleProperties(cell.style || '')
          },
          zIndex: 0 // Default z-index
        });
      }
    });
    
    return elements;
  }

  /**
   * Map cell style to element type
   * @param style Cell style string
   * @returns The mapped diagram element type
   */
  private mapCellStyleToType(style: string): DiagramElementType {
    if (!style) return DiagramElementType.RECTANGLE;
    
    // Check for circle style
    if (style.includes('shape=circle')) return DiagramElementType.CIRCLE;
    
    // Also handle ellipse as circle (alternative format)
    if (style.includes('shape=ellipse')) return DiagramElementType.CIRCLE;
    
    if (style.includes('shape=triangle')) return DiagramElementType.TRIANGLE;
    if (style.includes('shape=image')) return DiagramElementType.IMAGE;
    if (style.includes('shape=text')) return DiagramElementType.TEXT;
    if (style.includes('shape=line')) return DiagramElementType.LINE;
    
    return DiagramElementType.RECTANGLE; // Default
  }

  /**
   * Parse style string into properties object
   * @param style Cell style string
   * @returns Parsed element properties
   */
  private parseStyleProperties(style: string): DiagramElementProperties {
    if (!style) return {};
    
    const properties: DiagramElementProperties = {};
    const styleParts = style.split(';');
    
    styleParts.forEach(part => {
      const [key, value] = part.split('=');
      if (key && value) {
        switch (key.trim()) {
          case 'fillColor':
            properties.backgroundColor = value.trim();
            break;
          case 'strokeColor':
            properties.borderColor = value.trim();
            break;
          case 'strokeWidth':
            properties.borderWidth = parseInt(value.trim(), 10);
            break;
          case 'fontColor':
            properties.color = value.trim();
            break;
          case 'fontSize':
            properties.fontSize = parseInt(value.trim(), 10);
            break;
          case 'fontFamily':
            properties.fontFamily = value.trim();
            break;
          case 'opacity':
            properties.opacity = parseFloat(value.trim());
            break;
          case 'imageUrl':
            properties.imageUrl = value.trim();
            break;
          // Add more property mappings as needed
        }
      }
    });
    
    return properties;
  }
  
  // Helper function to generate UUID
  private generateUuid(): string {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
      const r = Math.random() * 16 | 0;
      const v = c === 'x' ? r : (r & 0x3 | 0x8);
      return v.toString(16);
    });
  }
}
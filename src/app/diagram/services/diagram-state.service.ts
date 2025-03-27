import { Injectable, signal, computed, effect } from '@angular/core';
import { 
  Diagram, 
  DiagramElement, 
  DiagramElementType, 
  DiagramProperties, 
  DiagramMetadata, 
  DiagramElementProperties,
  Position,
  Size
} from '../models/diagram.model';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { ErrorResponse } from '../../shared/types/common.types';
import { StorageFile } from '../../shared/services/storage/providers/storage-provider.interface';

/**
 * Signal-based state management service for diagrams
 * This replaces the NgRx store and facade pattern with Angular Signals
 */
@Injectable({
  providedIn: 'root'
})
export class DiagramStateService {
  // Core state signals
  readonly currentDiagram = signal<Diagram | null>(null);
  readonly isLoading = signal<boolean>(false);
  readonly error = signal<ErrorResponse | null>(null);
  readonly diagramList = signal<DiagramMetadata[]>([]);
  readonly elements = signal<DiagramElement[]>([]);
  readonly selectedElementId = signal<string | null>(null);
  readonly hasChanges = signal<boolean>(false);
  readonly currentFile = signal<StorageFile | null>(null);
  
  // UI state signals
  readonly showGrid = signal<boolean>(true);
  readonly gridSize = signal<number>(20);
  readonly zoomLevel = signal<number>(1);
  readonly backgroundColor = signal<string>('#ffffff');
  
  // Computed signals (replaces selectors)
  readonly selectedElement = computed(() => {
    const id = this.selectedElementId();
    return this.elements().find(el => el.id === id) || null;
  });
  
  readonly diagramForCanvas = computed(() => {
    return {
      elements: this.elements(),
      zoomLevel: this.zoomLevel(),
      showGrid: this.showGrid(),
      gridSize: this.gridSize(),
      selectedElementIds: this.selectedElementId() ? [this.selectedElementId()!] : [],
      backgroundColor: this.backgroundColor()
    };
  });

  constructor(private logger: LoggerService) {
    // Effect to synchronize elements with currentDiagram
    effect(() => {
      const diagram = this.currentDiagram();
      if (diagram) {
        this.elements.set(diagram.elements);
        this.backgroundColor.set(diagram.properties.backgroundColor || '#ffffff');
        this.gridSize.set(diagram.properties.gridSize || 20);
        this.showGrid.set(diagram.properties.snapToGrid || true);
      } else {
        this.elements.set([]);
      }
    });
  }

  /**
   * Sets the loading state
   */
  setLoading(loading: boolean): void {
    this.isLoading.set(loading);
  }

  /**
   * Sets an error state
   */
  setError(error: ErrorResponse | null): void {
    this.error.set(error);
  }

  /**
   * Sets the current diagram
   */
  setCurrentDiagram(diagram: Diagram | null): void {
    this.currentDiagram.set(diagram);
    this.hasChanges.set(false);
  }

  /**
   * Sets the diagram list
   */
  setDiagramList(diagrams: DiagramMetadata[]): void {
    this.diagramList.set(diagrams);
  }

  /**
   * Select an element by ID
   */
  selectElement(id: string | null): void {
    this.selectedElementId.set(id);
  }

  /**
   * Clear the current diagram
   */
  clearDiagram(): void {
    this.setCurrentDiagram(null);
    this.selectedElementId.set(null);
    this.hasChanges.set(false);
  }

  /**
   * Add a new element to the diagram
   */
  addElement(type: DiagramElementType, position: Position, size: Size, properties?: DiagramElementProperties): void {
    const elements = this.elements();
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
    } else if (type === DiagramElementType.PROCESS) {
      defaultProperties = {
        ...defaultProperties,
        backgroundColor: '#ffffff',
        borderColor: '#000000',
        elementType: 'process'
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
    const element: DiagramElement = {
      id: elementId,
      type,
      position,
      size,
      properties: {
        ...defaultProperties,
        ...properties
      },
      zIndex: elements.length
    };
    
    this.logger.debug(`Adding new element: ${JSON.stringify({
      id: element.id,
      type: element.type,
      position: element.position,
      size: element.size
    })}`, 'DiagramStateService');
    
    // Update elements
    this.elements.update(current => [...current, element]);
    this.updateCurrentDiagramElements();
    this.hasChanges.set(true);
  }

  /**
   * Remove an element from the diagram
   */
  removeElement(id: string): void {
    this.elements.update(elements => elements.filter(el => el.id !== id));
    this.updateCurrentDiagramElements();
    this.hasChanges.set(true);
    
    // If the currently selected element is deleted, clear selection
    if (this.selectedElementId() === id) {
      this.selectElement(null);
    }
  }

  /**
   * Update an element's properties
   */
  updateElement(id: string, changes: Partial<DiagramElement>): void {
    this.elements.update(elements => 
      elements.map(el => el.id === id ? { ...el, ...changes } : el)
    );
    
    this.updateCurrentDiagramElements();
    this.hasChanges.set(true);
  }

  /**
   * Toggle grid visibility
   */
  toggleGrid(show: boolean): void {
    this.showGrid.set(show);
    
    if (this.currentDiagram()) {
      this.currentDiagram.update(diagram => {
        if (diagram) {
          return {
            ...diagram,
            properties: {
              ...diagram.properties,
              snapToGrid: show
            }
          };
        }
        return diagram;
      });
      
      this.hasChanges.set(true);
    }
  }


  /**
   * Set current storage file
   */
  setCurrentFile(file: StorageFile | null): void {
    this.currentFile.set(file);
  }

  /**
   * Create a new diagram with default settings
   */
  createNewDiagram(name: string, properties?: DiagramProperties): Diagram {
    const defaultProps: DiagramProperties = {
      backgroundColor: '#ffffff',
      gridSize: 20,
      snapToGrid: true,
      ...properties
    };
    
    const newDiagram: Diagram = {
      id: this.generateUuid(),
      name: name || 'Untitled Diagram',
      elements: [],
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      version: 1,
      properties: defaultProps
    };
    
    this.setCurrentDiagram(newDiagram);
    
    return newDiagram;
  }

  // Private helper methods
  
  /**
   * Update the elements in the current diagram
   */
  private updateCurrentDiagramElements(): void {
    if (this.currentDiagram()) {
      this.currentDiagram.update(diagram => {
        if (diagram) {
          return {
            ...diagram,
            elements: this.elements(),
            updatedAt: new Date().toISOString()
          };
        }
        return diagram;
      });
    }
  }


  /**
   * Generate a unique ID
   */
  private generateUuid(): string {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
      const r = Math.random() * 16 | 0;
      const v = c === 'x' ? r : (r & 0x3 | 0x8);
      return v.toString(16);
    });
  }
  
  /**
   * Load diagram list from storage
   * This is a placeholder implementation - in a real app you would
   * connect this to your storage service
   */
  async loadDiagramList(): Promise<DiagramMetadata[]> {
    try {
      // For now, just set a simulated list of diagrams
      // In a real implementation, you would load this from storage
      const diagrams: DiagramMetadata[] = [
        {
          id: this.generateUuid(),
          name: 'Sample Diagram 1',
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
          lastOpenedAt: new Date().toISOString()
        },
        {
          id: this.generateUuid(),
          name: 'Sample Diagram 2',
          createdAt: new Date(Date.now() - 86400000).toISOString(), // 1 day ago
          updatedAt: new Date(Date.now() - 43200000).toISOString(), // 12 hours ago
          lastOpenedAt: new Date(Date.now() - 43200000).toISOString()
        }
      ];
      
      // Update the diagramList signal
      this.diagramList.set(diagrams);
      return diagrams;
    } catch (error) {
      this.logger.error('Error loading diagram list', 'DiagramStateService', error);
      return [];
    }
  }
}
import { Injectable } from '@angular/core';
import { DiagramElement } from '../store/models/diagram.model';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { Graph, Cell } from '@maxgraph/core';

// Define the DiagramGraph interface to type the Graph from MaxGraph
// This interface provides a common API surface for MaxGraph
// We're defining only the methods we actually use
export interface DiagramGraph {
  getView: () => { 
    getScale: () => number; 
    setScale: (scale: number) => void;
    refresh: () => void;
  };
  // Use model property instead of getModel method to match MaxGraph API
  model: {
    beginUpdate: () => void;
    endUpdate: () => void;
    getCell: (id: string) => Cell;
    getValue: (cell: Cell) => string | null;
    setValue: (cell: Cell, value: string) => void;
    getStyle: (cell: Cell) => string | null;
    setStyle: (cell: Cell, style: string) => void;
    setGeometry: (cell: Cell, geometry: any) => void;
  };
  getDefaultParent: () => Cell;
  getChildCells: (parent: Cell) => Cell[];
  isGridEnabled: () => boolean;
  setGridEnabled: (enabled: boolean) => void;
  zoomTo: (scale: number, center?: boolean) => void;
  clearSelection: () => void;
  setSelectionCells: (cells: Cell[]) => void;
  removeCells: (cells: Cell[]) => Cell[];
  insertVertex: (
    parent: Cell,
    id: string | null,
    value: string,
    x: number,
    y: number, 
    width: number, 
    height: number, 
    style?: string
  ) => Cell;
  insertEdge: (
    parent: Cell,
    id: string | null, 
    value: string, 
    source: Cell, 
    target: Cell, 
    style?: string
  ) => Cell;
  container: HTMLElement;
  sizeDidChange: () => void;
  gridSize: number;
  setConnectable: (connectable: boolean) => void;
  setCellsMovable: (movable: boolean) => void;
  setCellsResizable: (resizable: boolean) => void;
  setCellsEditable: (editable: boolean) => void;
  setCellsBendable: (bendable: boolean) => void;
  setTooltips: (enabled: boolean) => void;
  setAllowDanglingEdges: (allowDanglingEdges: boolean) => void;
  setSelectionEnabled: (enabled: boolean) => void;
  getSelectionCells: () => Cell[];
}

/**
 * DiagramRendererService handles the rendering logic for the diagram canvas
 * This service is separate from the component to improve maintainability and testability
 */
@Injectable({
  providedIn: 'root'
})
export class DiagramRendererService {
  constructor(private logger: LoggerService) {}

  /**
   * Update the diagram graph based on the state
   */
  updateGraph(graph: DiagramGraph, elements: DiagramElement[], settings: {
    zoomLevel: number;
    showGrid: boolean;
    gridSize: number;
    backgroundColor: string;
    selectedElementIds: string[];
  }): void {
    if (!graph) {
      this.logger.warn('Cannot update graph: graph is null', 'DiagramRendererService');
      return;
    }

    try {
      // Get the graph view
      const view = graph.getView();
      if (view) {
        // Update zoom level if needed and method exists
        if (typeof view.getScale === 'function' && view.getScale() !== settings.zoomLevel) {
          graph.zoomTo(settings.zoomLevel, true);
        }
      }
      
      // Update grid visibility if needed
      if (typeof graph.isGridEnabled === 'function' && 
          typeof graph.setGridEnabled === 'function' && 
          graph.isGridEnabled() !== settings.showGrid) {
        graph.setGridEnabled(settings.showGrid);
      }
      
      // Update grid size if needed
      if (graph.gridSize !== undefined && graph.gridSize !== settings.gridSize) {
        graph.gridSize = settings.gridSize;
      }
      
      // Update background color if needed
      if (graph.container && graph.container.style) {
        graph.container.style.backgroundColor = settings.backgroundColor;
      }
      
      // Only proceed with element updates if we have valid elements
      if (Array.isArray(elements) && elements.length > 0) {
        // Update selected elements
        this.updateSelectedElements(graph, settings.selectedElementIds);
        
        // Optimistic batched updates for elements
        this.batchUpdateElements(graph, elements);
      } else {
        this.logger.debug('No elements to update in diagram', 'DiagramRendererService');
      }
    } catch (error) {
      this.logger.error('Error updating diagram graph', 'DiagramRendererService', error);
    }
  }

  /**
   * Update selected elements
   */
  private updateSelectedElements(graph: DiagramGraph, selectedIds: string[]): void {
    if (!graph) return;
    
    try {
      // Check if we have the required methods
      if (typeof graph.clearSelection !== 'function' || 
          typeof graph.setSelectionCells !== 'function') {
        this.logger.warn('Selection methods not available in graph', 'DiagramRendererService');
        return;
      }
      
      // Clear current selection
      graph.clearSelection();
      
      if (selectedIds && selectedIds.length > 0) {
        const model = graph.model;
        if (!model || typeof model.getCell !== 'function') {
          this.logger.warn('Graph model not initialized properly', 'DiagramRendererService');
          return;
        }
        
        // Find cells by id and select them
        const cells = selectedIds
          .map(id => {
            try {
              return model.getCell(id);
            } catch (error) {
              return null;
            }
          })
          .filter(cell => cell !== null && cell !== undefined);
          
        if (cells.length > 0) {
          graph.setSelectionCells(cells);
        }
      }
    } catch (error) {
      this.logger.warn('Error updating selected elements', 'DiagramRendererService', error);
    }
  }

  /**
   * Perform batched updates to diagram elements
   * This is more efficient than updating each element individually
   */
  private batchUpdateElements(graph: DiagramGraph, elements: DiagramElement[]): void {
    if (!graph) {
      this.logger.warn('Cannot update elements: graph is null', 'DiagramRendererService');
      return;
    }
    
    if (!elements || !Array.isArray(elements) || elements.length === 0) {
      this.logger.debug('No elements to update in diagram', 'DiagramRendererService');
      return;
    }
    
    try {
      const model = graph.model;
      if (!model) {
        this.logger.warn('Graph model is null', 'DiagramRendererService');
        return;
      }
      
      if (typeof model.beginUpdate !== 'function' || typeof model.endUpdate !== 'function') {
        this.logger.warn('Graph model APIs not available (beginUpdate/endUpdate)', 'DiagramRendererService');
        return;
      }
      
      const parent = graph.getDefaultParent();
      if (!parent) {
        this.logger.warn('Unable to get default parent for graph', 'DiagramRendererService');
        return;
      }
      
      // Safely get existing cells
      let existingCells = [];
      try {
        existingCells = graph.getChildCells(parent) || [];
      } catch (error) {
        this.logger.warn('Error getting child cells from parent', 'DiagramRendererService', error);
        // Continue with empty array
      }
      
      // Create maps for faster lookups
      const existingCellsMap = new Map();
      
      // Safely handle each cell and its ID
      for (const cell of existingCells) {
        if (cell && typeof cell.getId === 'function') {
          try {
            const id = cell.getId();
            if (id) {
              existingCellsMap.set(id, cell);
            }
          } catch (error) {
            // Skip this cell if we can't get its ID
            this.logger.debug('Unable to get ID for cell', 'DiagramRendererService');
          }
        }
      }
      
      const elementIdsMap = new Set(
        elements.map(el => el.id)
      );
      
      try {
        // Begin batched update
        model.beginUpdate();
        
        // 1. Remove cells that no longer exist in the state
        const cellsToRemove = [];
        for (const cell of existingCells) {
          if (cell && typeof cell.getId === 'function') {
            try {
              const id = cell.getId();
              if (id && !elementIdsMap.has(id)) {
                cellsToRemove.push(cell);
              }
            } catch (error) {
              // Skip this cell
            }
          }
        }
        
        if (cellsToRemove.length > 0 && typeof graph.removeCells === 'function') {
          try {
            graph.removeCells(cellsToRemove);
          } catch (error) {
            this.logger.warn('Error removing cells', 'DiagramRendererService', error);
          }
        }
        
        // 2. Add or update elements
        for (const element of elements) {
          if (!element || !element.id) continue;
          
          try {
            const existingCell = existingCellsMap.get(element.id);
            
            if (!existingCell) {
              // Add new element
              this.addElementToGraph(graph, element);
            } else {
              // Update existing element if needed
              this.updateElementInGraph(graph, existingCell, element);
            }
          } catch (error) {
            this.logger.warn(`Error processing element ${element.id}`, 'DiagramRendererService', error);
            // Continue with next element
          }
        }
      } catch (error) {
        this.logger.warn('Error during batch update transaction', 'DiagramRendererService', error);
      } finally {
        // End batched update - do this in another try/catch to ensure it runs
        try {
          model.endUpdate();
        } catch (error) {
          this.logger.warn('Error ending batch update', 'DiagramRendererService', error);
        }
      }
    } catch (error) {
      this.logger.error('Error in batch update of elements', 'DiagramRendererService', error);
    }
  }

  /**
   * Add a new element to the graph
   */
  private addElementToGraph(graph: DiagramGraph, element: DiagramElement): void {
    if (!graph) {
      this.logger.warn('Cannot add element: graph is null', 'DiagramRendererService');
      return;
    }
    
    if (!element || !element.id) {
      this.logger.warn('Cannot add element: invalid element data', 'DiagramRendererService');
      return;
    }
    
    try {
      const parent = graph.getDefaultParent();
      if (!parent) {
        this.logger.warn('Unable to get default parent for adding elements', 'DiagramRendererService');
        return;
      }
      
      // Check if this element already exists in the graph
      let existingCell = null;
      try {
        existingCell = graph.model.getCell(element.id);
      } catch (e) {
        // Cell doesn't exist, which is fine for new elements
      }
      
      if (existingCell) {
        this.logger.debug(`Element ${element.id} already exists in graph, updating instead of adding`, 'DiagramRendererService');
        this.updateElementInGraph(graph, existingCell, element);
        return;
      }
      
      // Verify insertVertex method exists
      if (typeof graph.insertVertex !== 'function') {
        this.logger.warn('Graph insertVertex method not available', 'DiagramRendererService');
        return;
      }
      
      // Handle different element types
      switch (element.type) {
        case 'rectangle':
        case 'circle':
        case 'triangle':
        case 'text':
        case 'image':
          try {
            // Ensure we have valid position and size with fallbacks
            const x = element.position?.x || 0;
            const y = element.position?.y || 0;
            const width = element.size?.width || 100;
            const height = element.size?.height || 50;
            const text = element.properties?.text || '';
            
            // Create style string based on element type and properties
            const styleString = this.getStyleForElement(element);
            
            // Insert the vertex
            const cell = graph.insertVertex(
              parent,
              element.id,
              text,
              x,
              y,
              width,
              height,
              styleString
            );
            
            if (cell) {
              this.logger.debug(`Successfully added ${element.type} element with ID ${element.id}`, 'DiagramRendererService');
            } else {
              this.logger.warn(`Failed to add ${element.type} element with ID ${element.id}, null cell returned`, 'DiagramRendererService');
            }
          } catch (error) {
            this.logger.warn(`Error inserting vertex for element ${element.id}`, 'DiagramRendererService', error);
          }
          break;
          
        case 'connector':
        case 'line':
          // For connectors, we need to find the source and target nodes
          if (element.properties.sourceElementId && element.properties.targetElementId) {
            try {
              const model = graph.model;
              if (!model) {
                this.logger.warn('Graph model is null for edge creation', 'DiagramRendererService');
                return;
              }
              
              if (typeof model.getCell !== 'function') {
                this.logger.warn('Graph model getCell method not available', 'DiagramRendererService');
                return;
              }
              
              if (typeof graph.insertEdge !== 'function') {
                this.logger.warn('Graph insertEdge method not available', 'DiagramRendererService');
                return;
              }
              
              // Get source and target cells
              const source = model.getCell(element.properties.sourceElementId);
              const target = model.getCell(element.properties.targetElementId);
              
              if (!source) {
                this.logger.warn(`Source cell not found: ${element.properties.sourceElementId}`, 'DiagramRendererService');
                return;
              }
              
              if (!target) {
                this.logger.warn(`Target cell not found: ${element.properties.targetElementId}`, 'DiagramRendererService');
                return;
              }
              
              // Insert the edge
              graph.insertEdge(
                parent,
                element.id,
                element.properties.text || '',
                source,
                target,
                this.getStyleForElement(element)
              );
              
              this.logger.debug(`Added ${element.type} element with ID ${element.id}`, 'DiagramRendererService');
            } catch (error) {
              this.logger.warn(`Error inserting edge for element ${element.id}`, 'DiagramRendererService', error);
            }
          } else {
            this.logger.warn(`Connector missing source or target: ${element.id}`, 'DiagramRendererService');
          }
          break;
          
        default:
          this.logger.warn(`Unknown element type: ${element.type}`, 'DiagramRendererService');
          break;
      }
    } catch (error) {
      this.logger.error('Error adding element to graph', 'DiagramRendererService', error);
    }
  }

  /**
   * Update an existing element in the graph
   */
  private updateElementInGraph(graph: DiagramGraph, cell: Cell, element: DiagramElement): void {
    if (!graph) return;
    
    const model = graph.model;
    
    // Check if we need to update the geometry
    const isVertex = !cell.isEdge();
    if (isVertex) {
      const geometry = cell.getGeometry();
      
      if (geometry && (
          geometry.x !== element.position.x || 
          geometry.y !== element.position.y ||
          geometry.width !== element.size.width ||
          geometry.height !== element.size.height)) {
        
        const newGeometry = geometry.clone();
        newGeometry.x = element.position.x;
        newGeometry.y = element.position.y;
        newGeometry.width = element.size.width;
        newGeometry.height = element.size.height;
        
        model.setGeometry(cell, newGeometry);
      }
    }
    
    // Check if we need to update the value (text)
    const currentValue = model.getValue(cell) || '';
    const newText = element.properties.text || '';
    if (currentValue !== newText) {
      model.setValue(cell, newText);
    }
    
    // Check if we need to update the style
    const newStyle = this.getStyleForElement(element);
    const currentStyle = model.getStyle(cell) || '';
    if (currentStyle !== newStyle) {
      model.setStyle(cell, newStyle);
    }
  }

  /**
   * Get MaxGraph style string for an element
   */
  private getStyleForElement(element: DiagramElement): string {
    let style = '';
    
    switch (element.type) {
      case 'rectangle':
        style = 'shape=rectangle;';
        break;
      case 'circle':
        style = 'shape=circle;aspect=fixed;';
        break;
      case 'triangle':
        style = 'shape=triangle;';
        break;
      case 'text':
        style = 'shape=text;html=1;';
        break;
      case 'connector':
        style = 'edgeStyle=orthogonalEdgeStyle;curved=1;';
        break;
      case 'line':
        style = 'shape=line;';
        break;
      case 'image':
        style = 'shape=image;';
        break;
      default:
        style = 'shape=rectangle;';
        break;
    }
    
    // Add color styles
    if (element.properties.backgroundColor) {
      style += `fillColor=${element.properties.backgroundColor};`;
    }
    
    if (element.properties.borderColor) {
      style += `strokeColor=${element.properties.borderColor};`;
    }
    
    if (element.properties.borderWidth) {
      style += `strokeWidth=${element.properties.borderWidth};`;
    }
    
    if (element.properties.color) {
      style += `fontColor=${element.properties.color};`;
    }
    
    if (element.properties.fontFamily) {
      style += `fontFamily=${element.properties.fontFamily};`;
    }
    
    if (element.properties.fontSize) {
      style += `fontSize=${element.properties.fontSize};`;
    }
    
    if (element.properties.opacity) {
      style += `opacity=${element.properties.opacity};`;
    }
    
    if (element.properties.imageUrl) {
      style += `image=${element.properties.imageUrl};`;
    }
    
    return style;
  }
}
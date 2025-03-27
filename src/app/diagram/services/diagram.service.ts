import { Injectable, signal, computed } from '@angular/core';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { StorageService } from '../../shared/services/storage/storage.service';
import { StorageFile } from '../../shared/services/storage/providers/storage-provider.interface';
import { DiagramCell, DiagramData, DiagramService as DiagramServiceInterface } from './diagram-service';

// Re-export these interfaces for other services that import from diagram.service.ts
export type { DiagramCell, DiagramData } from './diagram-service';

// Import MaxGraph
import { Graph, Cell } from '@maxgraph/core';

@Injectable({
  providedIn: 'root'
})
export class DiagramService implements DiagramServiceInterface {
  private graph: Graph | null = null;
  
  // Signal-based state
  private diagramSignal = signal<DiagramData | null>(null);
  private dirtySignal = signal<boolean>(false);
  private fileSignal = signal<StorageFile | null>(null);
  
  // Expose signals
  readonly currentDiagram = this.diagramSignal;
  readonly isDirty = this.dirtySignal;
  readonly currentFile = this.fileSignal;

  constructor(
    private logger: LoggerService,
    private storageService: StorageService
  ) {
    this.logger.debug('DiagramService created', 'DiagramService');
  }

  /**
   * Initialize the diagram graph
   * @param container The HTML element to contain the graph
   * @returns The graph instance
   */
  initGraph(container: HTMLElement): Graph {
    try {
      // Create a new graph instance with MaxGraph
      // Set default parent container
      if (!container) {
        throw new Error('Container element is required for graph initialization');
      }
      
      // First make sure the container has the right CSS to display the graph properly
      container.style.position = 'relative';
      container.style.overflow = 'hidden';
      container.style.width = '100%';
      container.style.height = '100%';
      container.style.background = '#ffffff';
      
      this.graph = new Graph(container);
      
      if (!this.graph) {
        throw new Error('Failed to create graph instance');
      }
      
      // Configure common settings
      this.configureGraph(this.graph);
      
      // Create a blank diagram structure
      this.resetDiagram();
      
      this.logger.debug('Graph initialized', 'DiagramService');
      return this.graph;
    } catch (error) {
      this.logger.error('Error initializing graph', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Configure the graph with default settings
   * @param graph The graph to configure
   */
  private configureGraph(graph: Graph): void {
    try {
      // Basic interaction settings
      graph.setConnectable(true);      // Allow creating connections
      graph.setGridEnabled(true);      // Show grid
      graph.setCellsMovable(true);     // Allow moving cells
      graph.setCellsResizable(true);   // Allow resizing cells
      graph.setAllowDanglingEdges(false); // Don't allow dangling edges
      
      // Allow connecting to edges
      if (typeof graph.setConnectableEdges === 'function') {
        graph.setConnectableEdges(true);
      }
      
      // Allow self-connections
      if (typeof graph.setAllowLoops === 'function') {
        graph.setAllowLoops(true);
      }
      
      // Set default grid size
      graph.gridSize = 20;
      
      // Avoid accidental double clicks
      graph.setTooltips(true);
      
      // Ensure vertex labels can be edited
      graph.setCellsEditable(true);
      
      // Setup selection listener
      graph.addListener('click', (sender: any, evt: any) => {
        const cell = evt.getProperty('cell');
        this.logger.info(`Cell clicked: ${cell ? cell.getId() : 'none'}`, 'DiagramService');
        
        // Update selection in current diagram
        if (cell) {
          const updatedDiagram = {
            ...this.diagramSignal(),
            selectedCellId: cell.getId()
          };
          
          // Update diagram signal
          this.diagramSignal.set(updatedDiagram);
        }
      });
      
      // Enable rubberband selection - this may be called differently in MaxGraph
      // graph.setRubberband(true);
      
      // Allow connecting to edges - check if this method exists in MaxGraph
      if (typeof graph.setConnectableEdges === 'function') {
        graph.setConnectableEdges(true);
      }
      
      // Allow self-connections - check if this method exists in MaxGraph
      if (typeof graph.setAllowLoops === 'function') {
        graph.setAllowLoops(true);
      }
      
      // Enable edge bending
      graph.setCellsBendable(true);
      
      // Configure styles
      this.configureGraphStyles(graph);
      
      // Register custom shapes if needed
      this.registerCustomShapes();
      
      this.logger.debug('Graph configured', 'DiagramService');
    } catch (error) {
      this.logger.error('Error configuring graph', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Configure the styles for the graph
   * @param graph The graph to configure styles for
   */
  private configureGraphStyles(graph: Graph): void {
    try {
      const stylesheet = graph.getStylesheet();
      
      // Configure default styles for vertices and edges
      if (stylesheet) {
        const defaultVertexStyle = stylesheet.getDefaultVertexStyle();
        const defaultEdgeStyle = stylesheet.getDefaultEdgeStyle();
        
        // Set vertex style properties
        if (defaultVertexStyle) {
          defaultVertexStyle.strokeColor = '#333333';
          defaultVertexStyle.fillColor = '#ffffff';
          defaultVertexStyle.fontColor = '#333333';
          defaultVertexStyle.fontSize = 12;
          defaultVertexStyle.fontFamily = 'Arial';
          defaultVertexStyle.align = 'center';
          defaultVertexStyle.verticalAlign = 'middle';
          defaultVertexStyle.rounded = false;
        }
        
        // Set edge style properties
        if (defaultEdgeStyle) {
          defaultEdgeStyle.strokeColor = '#333333';
          defaultEdgeStyle.fontColor = '#333333';
          defaultEdgeStyle.fontSize = 10;
          defaultEdgeStyle.fontFamily = 'Arial';
          defaultEdgeStyle.align = 'center';
          defaultEdgeStyle.edgeStyle = 'orthogonalEdgeStyle';
          defaultEdgeStyle.endArrow = 'classic';
        }
      }
    } catch (error) {
      this.logger.warn('Error configuring graph styles', 'DiagramService', error);
      // Continue even if styles fail to configure
    }
  }

  /**
   * Register custom shapes for the graph
   */
  private registerCustomShapes(): void {
    try {
      // Currently just using the built-in shapes
      // Can register custom shapes if needed in the future
    } catch (error) {
      this.logger.error('Error registering custom shapes', 'DiagramService', error);
    }
  }

  /**
   * Reset the diagram to a blank state
   * If the graph is not initialized, still creates a blank diagram structure
   */
  resetDiagram(): void {
    // Create a blank diagram data structure that we'll use in both scenarios
    const blankDiagram: DiagramData = {
      title: 'Untitled Diagram',
      cells: [],
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      version: '1.0',
      properties: {
        backgroundColor: '#ffffff',
        gridSize: 20,
        snapToGrid: true
      }
    };
    
    // If graph is not initialized yet, just update the diagram model
    if (!this.graph) {
      this.logger.debug('Creating blank diagram (graph not yet initialized)', 'DiagramService');
      
      // Update diagram signal
      this.diagramSignal.set(blankDiagram);
      
      this.markDiagramClean();
      return;
    }
    
    try {
      // If graph is initialized, we'll also clear the visual graph
      const model = this.graph.model;
      
      if (!model || typeof model.beginUpdate !== 'function') {
        this.logger.warn('Graph model not properly initialized', 'DiagramService');
        // Still update the diagram model even if graph operations fail
        
        // Update diagram signal
        this.diagramSignal.set(blankDiagram);
        
        this.markDiagramClean();
        return;
      }
      
      // Start a batch update
      model.beginUpdate();
      
      try {
        const parent = this.graph.getDefaultParent();
        if (!parent) {
          this.logger.warn('Unable to get default parent', 'DiagramService');
          return;
        }
        
        // Remove all cells from the graph
        const cells = this.graph.getChildCells(parent);
        if (cells && cells.length > 0 && typeof this.graph.removeCells === 'function') {
          this.graph.removeCells(cells);
        }
        
        // Create a blank diagram
        const blankDiagram: DiagramData = {
          title: 'Untitled Diagram',
          cells: [],
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
          version: '1.0',
          properties: {
            backgroundColor: '#ffffff',
            gridSize: 20,
            snapToGrid: true
          }
        };
        
        // Update the current diagram
        // Update diagram signal
        this.diagramSignal.set(blankDiagram);
        
        this.markDiagramClean();
        
      } catch (innerError) {
        this.logger.warn('Error during diagram reset operation', 'DiagramService', innerError);
      } finally {
        // End the batch update
        try {
          model.endUpdate();
        } catch (endError) {
          this.logger.warn('Error ending model update', 'DiagramService', endError);
        }
      }
      
      this.logger.debug('Diagram reset to blank state', 'DiagramService');
    } catch (error) {
      this.logger.error('Error resetting diagram', 'DiagramService', error);
      
      // Fallback - still create a blank diagram structure
      const blankDiagram: DiagramData = {
        title: 'Untitled Diagram',
        cells: [],
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: '1.0',
        properties: {
          backgroundColor: '#ffffff',
          gridSize: 20,
          snapToGrid: true
        }
      };
      
      // Update diagram signal
      this.diagramSignal.set(blankDiagram);
      
      this.markDiagramClean();
    }
  }

  /**
   * Add a node to the graph
   * @param x X coordinate
   * @param y Y coordinate
   * @param width Width of the node
   * @param height Height of the node
   * @param label Label text
   * @param style Optional style string
   * @returns The created cell
   */
  addNode(x: number, y: number, width: number, height: number, label: string, style?: string): Cell {
    if (!this.graph) {
      throw new Error('Graph not initialized');
    }
    
    try {
      // We use our facade to internal model operations
      const model = (this.graph as any).model; // Access the model
      const parent = this.graph.getDefaultParent();
      let result: Cell;
      
      // Start a batch update
      model.beginUpdate();
      
      try {
        // Insert the vertex
        result = this.graph.insertVertex(parent, null, label, x, y, width, height, style as any);
      } finally {
        // End the batch update
        model.endUpdate();
      }
      
      // Mark the diagram as dirty
      this.markDiagramDirty();
      
      return result;
    } catch (error) {
      this.logger.error('Error adding node', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Add an edge between two nodes
   * @param source Source node
   * @param target Target node
   * @param label Optional label text
   * @param style Optional style string
   * @returns The created edge
   */
  addEdge(source: Cell, target: Cell, label?: string, style?: string): Cell {
    if (!this.graph) {
      throw new Error('Graph not initialized');
    }
    
    try {
      // We use our facade to internal model operations
      const model = (this.graph as any).model; // Access the model
      const parent = this.graph.getDefaultParent();
      let result: Cell;
      
      // Start a batch update
      model.beginUpdate();
      
      try {
        // Insert the edge
        result = this.graph.insertEdge(parent, null, label || '', source, target, style as any);
      } finally {
        // End the batch update
        model.endUpdate();
      }
      
      // Mark the diagram as dirty
      this.markDiagramDirty();
      
      return result;
    } catch (error) {
      this.logger.error('Error adding edge', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Delete the selected cells
   */
  deleteSelected(): void {
    if (!this.graph) {
      this.logger.warn('Graph not initialized during delete', 'DiagramService');
      return;
    }
    
    try {
      // First try to get cells from graph selection
      let cellsToDelete = this.graph.getSelectionCells();
      
      // If no cells are selected in the graph, check our tracked selectedCellId
      if (!cellsToDelete || cellsToDelete.length === 0) {
        const diagramData = this.diagramSignal();
        const selectedId = diagramData?.selectedCellId;
        
        if (selectedId) {
          this.logger.info(`Using stored selectedCellId for deletion: ${selectedId}`, 'DiagramService');
          
          // Try to find the cell by ID
          const model = this.graph.model;
          const cellById = model.getCell(selectedId);
          
          if (cellById) {
            cellsToDelete = [cellById];
          }
        } else {
          // If no cells selected in graph and no selectedCellId, just return silently
          this.logger.debug('No cells selected for deletion', 'DiagramService');
          return;
        }
      }
      
      // Log what we're about to delete
      this.logger.info(`Deleting ${cellsToDelete?.length || 0} cells`, 'DiagramService');
      
      if (cellsToDelete && cellsToDelete.length > 0) {
        // We use our facade to internal model operations
        const model = this.graph.model;
        
        // Start a batch update
        model.beginUpdate();
        
        try {
          // Store the IDs before deletion for later cleanup
          const idsToDelete = cellsToDelete.map(cell => cell.getId());
          this.logger.info(`Cell IDs to delete: ${idsToDelete.join(', ')}`, 'DiagramService');
          
          // Remove the selected cells from the graph
          this.graph.removeCells(cellsToDelete);
          
          // Mark the diagram as dirty
          this.markDiagramDirty();
          
          // Clear the selection ID in our diagram data
          const currentData = this.diagramSignal();
          if (currentData) {
            // Update the diagram data to remove these cells completely
            const updatedCells = currentData.cells.filter(cell => 
              !idsToDelete.includes(cell.id)
            );
            
            this.logger.info(`Removed ${currentData.cells.length - updatedCells.length} cells from diagram data`, 'DiagramService');
            
            // Update the diagram data with the cleaned cell list and make sure deleted cells won't reappear
            const updatedDiagram = {
              ...currentData,
              cells: updatedCells,
              selectedCellId: undefined
            };
            
            // Update diagram signal
            this.diagramSignal.set(updatedDiagram);
            
            // Make sure the graph state and our state model are consistent by exporting from the graph
            const exportedData = this.exportDiagram();
            if (exportedData && exportedData.cells) {
              this.logger.debug(`Synchronizing state after deletion: graph has ${exportedData.cells.length} cells`, 'DiagramService');
            }
          }
        } finally {
          // End the batch update
          model.endUpdate();
        }
      }
    } catch (error) {
      this.logger.error('Error deleting selected cells', 'DiagramService', error);
    }
  }

  /**
   * Get the current diagram state
   * @returns The current diagram data or null
   */
  getCurrentDiagram(): DiagramData | null {
    return this.diagramSignal();
  }

  /**
   * Check if the diagram has unsaved changes
   * @returns True if the diagram is dirty
   */
  isDiagramDirty(): boolean {
    return this.dirtySignal();
  }

  /**
   * Mark the diagram as dirty (has unsaved changes)
   */
  markDiagramDirty(): void {
    // Update isDirty signal
    this.dirtySignal.set(true);
  }

  /**
   * Mark the diagram as clean (no unsaved changes)
   */
  markDiagramClean(): void {
    // Update isDirty signal
    this.dirtySignal.set(false);
  }

  /**
   * Get the current file
   * @returns The current file or null
   */
  getCurrentFile(): StorageFile | null {
    return this.fileSignal();
  }

  /**
   * Export the current diagram to a data structure
   * @returns The diagram data
   */
  exportDiagram(): DiagramData {
    if (!this.graph) {
      throw new Error('Graph not initialized');
    }
    
    try {
      const cells: DiagramCell[] = [];
      const parent = this.graph.getDefaultParent();
      
      // Get cells from the graph
      const childCells = this.graph.getChildCells(parent);
      
      // Log the current state
      this.logger.debug(`Exporting diagram with ${childCells.length} cells from graph`, 'DiagramService');
      
      // Create a map of cell IDs for quick lookups
      const cellIdsInGraph = new Set<string>();
      
      // Process each cell from the graph
      childCells.forEach(cell => {
        if (cell.isVertex() || cell.isEdge()) {
          const cellId = cell.getId();
          cellIdsInGraph.add(cellId);
          
          const geometry = cell.getGeometry();
          
          const cellData: DiagramCell = {
            id: cellId,
            value: cell.getValue() || '',
            isVertex: cell.isVertex(),
            isEdge: cell.isEdge(),
            geometry: {
              x: geometry.x,
              y: geometry.y,
              width: geometry.width,
              height: geometry.height
            },
            style: cell.getStyle()
          };
          
          // Add source and target IDs for edges
          if (cell.isEdge()) {
            cellData.sourceId = cell.source?.getId();
            cellData.targetId = cell.target?.getId();
          }
          
          cells.push(cellData);
        }
      });
      
      // Get the current diagram data
      const currentData = this.diagramSignal();
      
      // Create the diagram data
      const diagramData: DiagramData = {
        id: currentData?.id,
        title: currentData?.title || 'Untitled Diagram',
        cells: cells, // Only use the cells currently in the graph
        createdAt: currentData?.createdAt || new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: currentData?.version || '1.0',
        properties: currentData?.properties || {}
      };
      
      // If the current diagram has a selected cell ID, preserve it if the cell still exists
      if (currentData?.selectedCellId && cellIdsInGraph.has(currentData.selectedCellId)) {
        diagramData.selectedCellId = currentData.selectedCellId;
      }
      
      return diagramData;
    } catch (error) {
      this.logger.error('Error exporting diagram', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Import diagram data
   * @param data The diagram data to import
   */
  importDiagram(data: DiagramData): void {
    if (!this.graph) {
      throw new Error('Graph not initialized');
    }
    
    try {
      // Validate diagram data
      if (!data || !data.cells) {
        this.logger.warn('Invalid diagram data for import', 'DiagramService');
        data = {
          title: 'Untitled Diagram',
          cells: [],
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
          version: '1.0',
          properties: {
            backgroundColor: '#ffffff',
            gridSize: 20,
            snapToGrid: true
          }
        };
      }
      
      // Log import
      this.logger.info(`Importing diagram: ${data.title} with ${data.cells.length} cells`, 'DiagramService');
      
      // We use our facade to internal model operations
      const model = this.graph.model; // Access the model directly
      const parent = this.graph.getDefaultParent();
      
      // Start a batch update
      model.beginUpdate();
      
      try {
        // Clear existing cells
        const existingCells = this.graph.getChildCells(parent);
        if (existingCells && existingCells.length > 0) {
          this.logger.debug(`Removing ${existingCells.length} existing cells before import`, 'DiagramService');
          this.graph.removeCells(existingCells);
        }
        
        // Maps to store cells by ID for edge creation
        const cellMap = new Map<string, Cell>();
        
        // First pass: create all vertices
        data.cells.forEach(cellData => {
          if (cellData.isVertex) {
            try {
              const vertex = this.graph.insertVertex(
                parent,
                cellData.id,
                cellData.value,
                cellData.geometry.x,
                cellData.geometry.y,
                cellData.geometry.width,
                cellData.geometry.height,
                cellData.style as any
              );
              
              cellMap.set(cellData.id, vertex);
            } catch (vertexError) {
              this.logger.warn(`Error creating vertex ${cellData.id}`, 'DiagramService', vertexError);
            }
          }
        });
        
        // Second pass: create all edges
        data.cells.forEach(cellData => {
          if (cellData.isEdge && cellData.sourceId && cellData.targetId) {
            try {
              const sourceCell = cellMap.get(cellData.sourceId);
              const targetCell = cellMap.get(cellData.targetId);
              
              if (sourceCell && targetCell) {
                const edge = this.graph.insertEdge(
                  parent,
                  cellData.id,
                  cellData.value,
                  sourceCell,
                  targetCell,
                  cellData.style as any
                );
                
                cellMap.set(cellData.id, edge);
              } else {
                this.logger.warn(`Missing source or target cell for edge ${cellData.id}`, 'DiagramService');
              }
            } catch (edgeError) {
              this.logger.warn(`Error creating edge ${cellData.id}`, 'DiagramService', edgeError);
            }
          }
        });
        
        // Verify all cells were created correctly
        const importedCells = this.graph.getChildCells(parent);
        this.logger.info(`Successfully imported ${importedCells.length} cells`, 'DiagramService');
        
        // Synchronize the current diagram data structure with what's actually in the graph
        const currentData = {
          ...data,
          // Ensure cells list exactly matches what's in the graph
          cells: this.exportDiagram().cells
        };
        
        // Update current diagram
        // Update diagram signal
        this.diagramSignal.set(currentData);
        this.markDiagramClean();
      } finally {
        // End the batch update
        model.endUpdate();
      }
      
      this.logger.debug('Diagram imported', 'DiagramService');
    } catch (error) {
      this.logger.error('Error importing diagram', 'DiagramService', error);
    }
  }

  /**
   * Save the current diagram
   * @param filename Optional filename
   * @returns Promise resolving to the saved file
   */
  async saveDiagram(filename?: string): Promise<StorageFile> {
    try {
      // Export the current diagram
      const diagramData = this.exportDiagram();
      const jsonData = JSON.stringify(diagramData);
      
      let result: StorageFile;
      
      // Check if we have an existing file
      const currentFile = this.fileSignal();
      
      if (currentFile && !filename) {
        // Update existing file
        await this.storageService.saveFile(currentFile.id, jsonData);
        result = currentFile;
      } else {
        // Create a new file
        const name = filename || `${diagramData.title || 'Untitled Diagram'}.json`;
        result = await this.storageService.createFile(name, jsonData);
        
        // Update current file signal
        this.fileSignal.set(result);
      }
      
      // Mark the diagram as clean
      this.markDiagramClean();
      
      this.logger.debug('Diagram saved', 'DiagramService');
      return result;
    } catch (error) {
      this.logger.error('Error saving diagram', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Load a diagram by ID
   * @param id The diagram ID to load
   */
  async loadDiagram(id: string): Promise<void> {
    try {
      // Load the file from storage
      const jsonData = await this.storageService.loadFile(id);
      
      // Parse the diagram data
      const diagramData = JSON.parse(jsonData) as DiagramData;
      
      // Import the diagram
      this.importDiagram(diagramData);
      
      // Update the current file
      const files = await this.storageService.listFiles();
      const currentFile = files.find(file => file.id === id);
      
      if (currentFile) {
        // Update current file signal
        this.fileSignal.set(currentFile);
      }
      
      this.logger.debug('Diagram loaded', 'DiagramService');
    } catch (error) {
      this.logger.error('Error loading diagram', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Load the list of diagrams
   * @returns Promise resolving to the list of diagram files
   */
  async loadDiagramList(): Promise<StorageFile[]> {
    try {
      // Get the list of files
      const files = await this.storageService.listFiles();
      
      // Filter for diagram files (JSON)
      const diagramFiles = files.filter(file => 
        file.mimeType === 'application/json' || 
        file.name.endsWith('.json')
      );
      
      return diagramFiles;
    } catch (error) {
      this.logger.error('Error loading diagram list', 'DiagramService', error);
      throw error;
    }
  }
}
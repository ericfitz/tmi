import { Injectable } from '@angular/core';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { BehaviorSubject, Observable } from 'rxjs';
import { StorageService } from '../../shared/services/storage/storage.service';
import { StorageFile } from '../../shared/services/storage/providers/storage-provider.interface';
import { 
  Graph, 
  Cell,
  RubberBandHandler as RubberBand,
  Constants,
  PerimeterUtil,
  Edges,
  CodecRegistry
} from '@maxgraph/core';

export interface DiagramData {
  cells: any[];
  // Additional metadata for the diagram
  title?: string;
  description?: string;
  createdAt?: string;
  updatedAt?: string;
  version?: string;
}

@Injectable({
  providedIn: 'root'
})
export class DiagramService {
  private graph: mx.Graph | null = null;
  private currentDiagram = new BehaviorSubject<DiagramData | null>(null);
  private isDirty = new BehaviorSubject<boolean>(false);
  private currentFile = new BehaviorSubject<StorageFile | null>(null);

  constructor(
    private logger: LoggerService,
    private storageService: StorageService
  ) {}

  /**
   * Initialize a new graph with the provided container
   */
  initGraph(container: HTMLElement): mx.Graph {
    this.logger.debug('Initializing diagram graph', 'DiagramService');
    
    try {
      // Create a graph model
      const model = new mx.Model();
      
      // Create the graph with the container and model
      this.graph = new mx.Graph(container, model);
      
      // Configure basic graph settings
      this.configureGraph();
      
      // Initialize with a blank diagram
      this.resetDiagram();
      
      this.logger.info('Diagram graph initialized', 'DiagramService');
      return this.graph;
    } catch (error) {
      this.logger.error('Failed to initialize diagram graph', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Configure graph settings
   */
  private configureGraph(): void {
    if (!this.graph) {
      return;
    }
    
    // Enable rubberband selection
    new RubberBand(this.graph);
    
    // Disable connections to locked cells
    this.graph.setCellsLocked(false);
    
    // Allow connections between edges
    this.graph.setAllowDanglingEdges(false);
    
    // Allow connection to create new edges
    this.graph.setConnectable(true);
    
    // Allow cells to be moved
    this.graph.setCellsMovable(true);
    
    // Allow edges to be moved
    this.graph.setEdgesMovable(true);
    
    // Allow cells to be resized
    this.graph.setCellsResizable(true);
    
    // Allow cells to be disconnected
    this.graph.setDisconnectOnMove(false);
    
    // Allow dropping cells
    this.graph.setDropEnabled(true);
    
    // Configure default styles
    this.configureStyles();
  }

  /**
   * Configure default styles for the graph
   */
  private configureStyles(): void {
    if (!this.graph) {
      return;
    }
    
    // Default vertex style
    let style = this.graph.getStylesheet().getDefaultVertexStyle();
    style[Constants.STYLE_SHAPE] = Constants.SHAPE_RECTANGLE;
    style[Constants.STYLE_PERIMETER] = PerimeterUtil.RectanglePerimeter;
    style[Constants.STYLE_ROUNDED] = false;
    style[Constants.STYLE_FILLCOLOR] = '#FFFFFF';
    style[Constants.STYLE_STROKECOLOR] = '#000000';
    style[Constants.STYLE_FONTCOLOR] = '#000000';
    
    // Default edge style
    style = this.graph.getStylesheet().getDefaultEdgeStyle();
    style[Constants.STYLE_EDGE] = Edges.EdgeStyle.ElbowConnector;
    style[Constants.STYLE_STROKECOLOR] = '#000000';
    style[Constants.STYLE_FONTCOLOR] = '#000000';
    style[Constants.STYLE_ROUNDED] = true;
    
    // Apply the styles
    this.graph.refresh();
  }

  /**
   * Reset the current diagram to a blank state
   */
  resetDiagram(): void {
    if (!this.graph) {
      return;
    }
    
    // Begin update
    this.graph.getModel().beginUpdate();
    
    try {
      // Clear the graph
      this.graph.removeCells(this.graph.getChildCells(this.graph.getDefaultParent()));
      
      // Set a new blank diagram data
      const blankDiagram: DiagramData = {
        cells: [],
        title: 'Untitled Diagram',
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: '1.0'
      };
      
      this.currentDiagram.next(blankDiagram);
      this.currentFile.next(null);
      this.isDirty.next(false);
      
      this.logger.info('Diagram reset to blank state', 'DiagramService');
    } finally {
      // End update
      this.graph.getModel().endUpdate();
    }
  }

  /**
   * Add a node to the graph
   */
  addNode(x: number, y: number, width: number, height: number, label: string, style?: string): mx.Cell {
    if (!this.graph) {
      throw new Error('Graph not initialized');
    }
    
    // Begin update
    this.graph.getModel().beginUpdate();
    
    try {
      // Insert the vertex
      const parent = this.graph.getDefaultParent();
      const vertex = this.graph.insertVertex(parent, null, label, x, y, width, height, style);
      
      // Mark as dirty
      this.isDirty.next(true);
      
      this.logger.debug(`Node added: ${label}`, 'DiagramService');
      return vertex;
    } finally {
      // End update
      this.graph.getModel().endUpdate();
    }
  }

  /**
   * Add an edge between two nodes
   */
  addEdge(source: mx.Cell, target: mx.Cell, label: string, style?: string): mx.Cell {
    if (!this.graph) {
      throw new Error('Graph not initialized');
    }
    
    // Begin update
    this.graph.getModel().beginUpdate();
    
    try {
      // Insert the edge
      const parent = this.graph.getDefaultParent();
      const edge = this.graph.insertEdge(parent, null, label, source, target, style);
      
      // Mark as dirty
      this.isDirty.next(true);
      
      this.logger.debug(`Edge added between ${source.getValue()} and ${target.getValue()}`, 'DiagramService');
      return edge;
    } finally {
      // End update
      this.graph.getModel().endUpdate();
    }
  }

  /**
   * Delete selected cells
   */
  deleteSelected(): void {
    if (!this.graph) {
      return;
    }
    
    // Get selected cells
    const cells = this.graph.getSelectionCells();
    
    if (cells.length === 0) {
      return;
    }
    
    // Begin update
    this.graph.getModel().beginUpdate();
    
    try {
      // Remove the cells
      this.graph.removeCells(cells);
      
      // Mark as dirty
      this.isDirty.next(true);
      
      this.logger.debug(`Deleted ${cells.length} cells`, 'DiagramService');
    } finally {
      // End update
      this.graph.getModel().endUpdate();
    }
  }

  /**
   * Export the current diagram as JSON
   */
  exportDiagram(): DiagramData {
    if (!this.graph) {
      throw new Error('Graph not initialized');
    }
    
    // For now, we'll use a simplified approach since CodecRegistry is different
    // in the new API version
    const cells = this.graph.getChildCells(this.graph.getDefaultParent());
    
    // Serialize cells to a simple format
    const serializedCells = cells.map((cell: Cell) => ({
      id: cell.getId(),
      value: cell.getValue(),
      geometry: {
        x: cell.getGeometry().getX(),
        y: cell.getGeometry().getY(),
        width: cell.getGeometry().getWidth(),
        height: cell.getGeometry().getHeight()
      },
      style: cell.getStyle(),
      isVertex: cell.isVertex(),
      isEdge: cell.isEdge(),
      sourceId: cell.isEdge() ? cell.getSource().getId() : null,
      targetId: cell.isEdge() ? cell.getTarget().getId() : null
    }));
    
    // Get the current diagram data
    const diagramData = this.currentDiagram.getValue() || {
      cells: [],
      title: 'Untitled Diagram',
      createdAt: new Date().toISOString(),
      version: '1.0'
    };
    
    // Update the diagram data
    diagramData.cells = serializedCells;
    diagramData.updatedAt = new Date().toISOString();
    
    this.logger.debug('Diagram exported', 'DiagramService');
    return diagramData;
  }

  /**
   * Import a diagram from JSON
   */
  importDiagram(diagramData: DiagramData): void {
    if (!this.graph) {
      throw new Error('Graph not initialized');
    }
    
    // Begin update
    const model = this.graph.getModel();
    model.beginUpdate();
    
    try {
      // Clear the graph
      this.graph.removeCells(this.graph.getChildCells(this.graph.getDefaultParent()));
      
      // If there are cells, import them
      if (diagramData.cells && Array.isArray(diagramData.cells)) {
        const parent = this.graph.getDefaultParent();
        const verticesMap = new Map<string, Cell>();
        
        // First pass: create vertices
        diagramData.cells.forEach((cell: any) => {
          if (cell.isVertex) {
            const vertex = this.graph.insertVertex(
              parent,
              cell.id,
              cell.value,
              cell.geometry.x,
              cell.geometry.y,
              cell.geometry.width,
              cell.geometry.height,
              cell.style
            );
            verticesMap.set(cell.id, vertex);
          }
        });
        
        // Second pass: create edges
        diagramData.cells.forEach((cell: any) => {
          if (cell.isEdge) {
            const source = verticesMap.get(cell.sourceId);
            const target = verticesMap.get(cell.targetId);
            
            if (source && target) {
              this.graph.insertEdge(
                parent,
                cell.id,
                cell.value,
                source,
                target,
                cell.style
              );
            }
          }
        });
      }
      
      // Update the current diagram
      this.currentDiagram.next(diagramData);
      this.isDirty.next(false);
      
      this.logger.info('Diagram imported', 'DiagramService');
    } catch (error) {
      this.logger.error('Failed to import diagram', 'DiagramService', error);
      throw error;
    } finally {
      // End update
      model.endUpdate();
    }
  }

  /**
   * Save the current diagram to storage
   */
  async saveDiagram(fileName?: string): Promise<StorageFile> {
    // Export the diagram
    const diagramData = this.exportDiagram();
    
    // Convert to JSON
    const json = JSON.stringify(diagramData);
    
    try {
      let file = this.currentFile.getValue();
      
      // If we have a current file, update it
      if (file) {
        await this.storageService.saveFile(file.id, json);
        this.logger.info(`Diagram saved: ${file.name}`, 'DiagramService');
      } 
      // Otherwise create a new file
      else {
        file = await this.storageService.createFile(fileName || 'Untitled Diagram.json', json);
        this.currentFile.next(file);
        this.logger.info(`Diagram created: ${file.name}`, 'DiagramService');
      }
      
      // Mark as clean
      this.isDirty.next(false);
      
      return file;
    } catch (error) {
      this.logger.error('Failed to save diagram', 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Load a diagram from storage
   */
  async loadDiagram(fileId: string): Promise<void> {
    try {
      // Load the file content
      const content = await this.storageService.loadFile(fileId);
      
      // Parse the JSON
      const diagramData = JSON.parse(content) as DiagramData;
      
      // Import the diagram
      this.importDiagram(diagramData);
      
      // Update the current file
      const files = await this.storageService.listFiles();
      const file = files.find(f => f.id === fileId);
      
      if (file) {
        this.currentFile.next(file);
      }
      
      this.logger.info(`Diagram loaded: ${fileId}`, 'DiagramService');
    } catch (error) {
      this.logger.error(`Failed to load diagram: ${fileId}`, 'DiagramService', error);
      throw error;
    }
  }

  /**
   * Get the current diagram
   */
  getCurrentDiagram(): DiagramData | null {
    return this.currentDiagram.getValue();
  }

  /**
   * Get the current diagram as observable
   */
  get currentDiagram$(): Observable<DiagramData | null> {
    return this.currentDiagram.asObservable();
  }

  /**
   * Check if the diagram has unsaved changes
   */
  isDiagramDirty(): boolean {
    return this.isDirty.getValue();
  }

  /**
   * Get dirty state as observable
   */
  get isDirty$(): Observable<boolean> {
    return this.isDirty.asObservable();
  }

  /**
   * Get the current file
   */
  getCurrentFile(): StorageFile | null {
    return this.currentFile.getValue();
  }

  /**
   * Get the current file as observable
   */
  get currentFile$(): Observable<StorageFile | null> {
    return this.currentFile.asObservable();
  }
}
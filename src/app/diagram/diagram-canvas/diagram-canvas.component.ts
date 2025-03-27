import { 
  Component, 
  AfterViewInit, 
  ViewChild, 
  ElementRef, 
  OnDestroy, 
  ChangeDetectionStrategy,
  OnInit,
  inject,
  computed,
  effect,
  untracked
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { DiagramService } from '../services/diagram.service';
import { DiagramRendererService } from '../services/diagram-renderer.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { DiagramStateService } from '../services/diagram-state.service';
import { DiagramElement, DiagramElementProperties } from '../models/diagram.model';
// Import DiagramGraph from renderer service to ensure type compatibility
import { DiagramGraph } from '../services/diagram-renderer.service';

interface CanvasState {
  elements: DiagramElement[];
  zoomLevel: number;
  showGrid: boolean;
  gridSize: number;
  selectedElementIds: string[];
  backgroundColor: string;
}

@Component({
  selector: 'app-diagram-canvas',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './diagram-canvas.component.html',
  styleUrls: ['./diagram-canvas.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class DiagramCanvasComponent implements OnInit, AfterViewInit, OnDestroy {
  @ViewChild('diagramContainer', { static: true }) diagramContainer!: ElementRef;
  
  // Inject services
  private diagramState = inject(DiagramStateService);
  private diagramService = inject(DiagramService);
  private diagramRenderer = inject(DiagramRendererService);
  private logger = inject(LoggerService);
  
  private graph: DiagramGraph | null = null;
  private resizeObserver: ResizeObserver | null = null;
  
  // Computed state derived from signals
  private canvasState = computed<CanvasState>(() => {
    return {
      elements: this.diagramState.elements(),
      zoomLevel: this.diagramState.zoomLevel(),
      showGrid: this.diagramState.showGrid(),
      gridSize: this.diagramState.gridSize(),
      selectedElementIds: this.diagramState.selectedElementId() ? 
        [this.diagramState.selectedElementId()!] : [],
      backgroundColor: this.diagramState.backgroundColor()
    };
  });
  
  // Create the effect during component creation (injection time)
  // This is in the injection context
  private diagramEffect = effect(() => {
    // Get the latest state
    const state = this.canvasState();
    
    // Use untracked to avoid triggering this effect when calling updateDiagramFromState
    untracked(() => {
      // Only update if we have a graph and elements
      if (this.graph && state.elements) {
        this.updateDiagramFromState(state);
      }
    });
  });

  ngOnInit(): void {
    // No need to create an effect here, it's already created during injection
  }

  ngAfterViewInit(): void {
    // Initialize the diagram
    this.initializeDiagram();
    this.setupResizeObserver();
  }

  ngOnDestroy(): void {
    // Clean up resize observer
    if (this.resizeObserver) {
      this.resizeObserver.disconnect();
    }
  }

  /**
   * Initialize the diagram canvas
   */
  private initializeDiagram(): void {
    try {
      // Get the container element
      const container = this.diagramContainer.nativeElement;
      if (!container) {
        this.logger.error('Diagram container not found', 'DiagramCanvasComponent');
        return;
      }
      
      // Ensure the container has proper styles for the diagram
      container.style.position = 'relative';
      container.style.overflow = 'hidden';
      container.style.width = '100%';
      container.style.height = '100%'; 
      container.style.backgroundColor = '#ffffff';
      container.style.border = '1px solid #e0e0e0';
      container.style.borderRadius = '4px';
      
      // Add error handling wrapper
      setTimeout(() => {
        try {
          // Initialize the graph with a delay to ensure DOM is ready
          this.graph = this.diagramService.initGraph(container) as unknown as DiagramGraph;
          
          if (!this.graph) {
            throw new Error('Failed to initialize graph - graph object is null');
          }
          
          // Setup click event handler
          this.setupClickHandler(this.graph);
          
          // Get initial state and update the diagram
          this.updateDiagramFromState(this.canvasState());
          
          this.logger.info('Diagram canvas initialized', 'DiagramCanvasComponent');
        } catch (graphError) {
          // Handle graph initialization failure
          this.logger.warn('Failed to initialize diagram graph, creating a fallback visual element', 'DiagramCanvasComponent', graphError);
          
          // Create a fallback visual element to show something
          this.createFallbackCanvas(container);
        }
      }, 100); // Small delay to ensure DOM rendering is complete
    } catch (error) {
      this.logger.error('Failed to initialize diagram canvas', 'DiagramCanvasComponent', error);
    }
  }
  
  /**
   * Setup click handler for the graph
   */
  private setupClickHandler(graph: DiagramGraph): void {
    try {
      // Remove all existing click listeners to prevent duplicates
      try {
        // This is not in the interface, but might be available on the actual graph object
        if (typeof (graph as any).removeListeners === 'function') {
          (graph as any).removeListeners('click');
        }
      } catch (e) {
        this.logger.debug('Could not remove existing listeners (this is fine if first init)', 'DiagramCanvasComponent');
      }
      
      // Add click handler to graph with a higher priority to override existing handlers
      graph.addListener('click', (sender: any, evt: any) => {
        const cell = evt.getProperty('cell');
        
        if (cell) {
          const cellId = cell.getId();
          this.logger.debug(`Graph cell clicked: ${cellId}`, 'DiagramCanvasComponent');
          
          // Update DiagramStateService
          this.diagramState.selectElement(cellId);
          
          // Update DiagramService selection
          if (this.diagramService.getCurrentDiagram()) {
            const currentDiagram = this.diagramService.getCurrentDiagram();
            currentDiagram.selectedCellId = cellId;
            
            // Force update to trigger changes
            this.diagramService.markDiagramDirty();
            this.diagramService.markDiagramClean();
          }
          
          // Set selection in graph (this is important)
          try {
            if (typeof graph.clearSelection === 'function' && 
                typeof graph.setSelectionCells === 'function') {
              graph.clearSelection();
              const model = graph.model;
              if (model && typeof model.getCell === 'function') {
                const graphCell = model.getCell(cellId);
                if (graphCell) {
                  graph.setSelectionCells([graphCell]);
                }
              }
            }
          } catch (e) {
            this.logger.warn('Error updating graph selection', 'DiagramCanvasComponent', e);
          }
        } else {
          // Clicked on the background
          this.logger.debug('Background clicked, clearing selection', 'DiagramCanvasComponent');
          
          // Clear selection in both services
          this.diagramState.selectElement(null);
          
          if (this.diagramService.getCurrentDiagram()) {
            const currentDiagram = this.diagramService.getCurrentDiagram();
            currentDiagram.selectedCellId = undefined;
          }
          
          // Clear selection in graph
          if (typeof graph.clearSelection === 'function') {
            graph.clearSelection();
          }
        }
      });
    } catch (error) {
      this.logger.warn('Error setting up click handler', 'DiagramCanvasComponent', error);
    }
  }
  
  /**
   * Create a fallback canvas when graph initialization fails
   */
  private createFallbackCanvas(container: HTMLElement): void {
    // Clear the container
    container.innerHTML = '';
    
    // Create a message element
    const messageDiv = document.createElement('div');
    messageDiv.className = 'fallback-message';
    messageDiv.textContent = 'Diagram visualization not available in this environment.';
    
    // Style the message
    messageDiv.style.textAlign = 'center';
    messageDiv.style.padding = '20px';
    messageDiv.style.color = '#666';
    messageDiv.style.backgroundColor = '#f8f8f8';
    messageDiv.style.border = '1px dashed #ccc';
    messageDiv.style.borderRadius = '4px';
    messageDiv.style.marginTop = '40px';
    
    // Add the message to the container
    container.appendChild(messageDiv);
  }

  /**
   * Set up observer to handle container resizing
   */
  private setupResizeObserver(): void {
    // Create a resize observer to handle container size changes
    this.resizeObserver = new ResizeObserver(() => {
      if (this.graph) {
        this.graph.sizeDidChange();
      }
    });
    
    // Observe the container
    this.resizeObserver.observe(this.diagramContainer.nativeElement);
  }
  
  /**
   * Update diagram from state
   */
  private updateDiagramFromState(state: CanvasState): void {
    if (!this.graph || !state.elements) return;
    
    // Use memoization for expensive operations
    this.updateGraph(state);
  }
  
  /**
   * Memoized function to update the graph
   * This helps with performance by avoiding unnecessary updates
   */
  private updateGraph = this.memoizeGraphUpdate((state: CanvasState) => {
    // Delegate rendering to the renderer service
    if (this.graph) {
      try {
        this.diagramRenderer.updateGraph(this.graph, state.elements, {
          zoomLevel: state.zoomLevel,
          showGrid: state.showGrid,
          gridSize: state.gridSize,
          backgroundColor: state.backgroundColor,
          selectedElementIds: state.selectedElementIds
        });
      } catch (error) {
        this.logger.error('Error updating diagram graph', 'DiagramCanvasComponent', error);
      }
    }
  });
  
  /**
   * Create a memoize function to avoid unnecessary graph updates
   */
  private memoizeGraphUpdate<T extends (state: CanvasState) => void>(fn: T): T {
    const cache = {
      lastState: null as CanvasState | null,
      lastResult: null as unknown
    };
    
    return (function(this: any, state: CanvasState) {
      // Check if state is the same reference (performance optimization)
      if (cache.lastState === state) {
        return cache.lastResult;
      }
      
      // Targeted deep comparison of relevant state properties
      const elementsChanged = this.areElementsChanged(
        cache.lastState?.elements || [], 
        state.elements
      );
      
      const stateChanged = !cache.lastState || 
                          cache.lastState.zoomLevel !== state.zoomLevel ||
                          cache.lastState.showGrid !== state.showGrid ||
                          cache.lastState.gridSize !== state.gridSize ||
                          cache.lastState.backgroundColor !== state.backgroundColor ||
                          !this.areArraysEqual(cache.lastState.selectedElementIds, state.selectedElementIds) ||
                          elementsChanged;
      
      if (stateChanged) {
        cache.lastResult = fn.call(this, state);
        // Create a deep copy of the state to avoid mutation issues
        cache.lastState = {
          ...state,
          elements: [...state.elements],
          selectedElementIds: [...state.selectedElementIds]
        };
      }
      
      return cache.lastResult;
    }) as any as T;
  }
  
  /**
   * Compare two arrays of diagram elements for equality
   * Much more efficient than using JSON.stringify
   */
  private areElementsChanged(oldElements: DiagramElement[], newElements: DiagramElement[]): boolean {
    // Quick length check
    if (oldElements.length !== newElements.length) {
      return true;
    }
    
    // Build a map of the old elements for O(1) lookup
    const oldElementMap = new Map<string, DiagramElement>();
    for (const element of oldElements) {
      oldElementMap.set(element.id, element);
    }
    
    // Check if any element is changed or new
    for (const newElement of newElements) {
      const oldElement = oldElementMap.get(newElement.id);
      
      // Element doesn't exist in old array or has changed properties
      if (!oldElement || 
          !this.arePositionsEqual(oldElement.position, newElement.position) ||
          !this.areSizesEqual(oldElement.size, newElement.size) ||
          !this.arePropertiesEqual(oldElement.properties, newElement.properties)) {
        return true;
      }
    }
    
    return false;
  }
  
  /**
   * Compare two arrays for equality
   */
  private areArraysEqual<T>(a: T[], b: T[]): boolean {
    if (a.length !== b.length) return false;
    
    for (let i = 0; i < a.length; i++) {
      if (a[i] !== b[i]) return false;
    }
    
    return true;
  }
  
  /**
   * Compare two positions for equality
   */
  private arePositionsEqual(pos1: { x: number, y: number }, pos2: { x: number, y: number }): boolean {
    return pos1.x === pos2.x && pos1.y === pos2.y;
  }
  
  /**
   * Compare two sizes for equality
   */
  private areSizesEqual(size1: { width: number, height: number }, size2: { width: number, height: number }): boolean {
    return size1.width === size2.width && size1.height === size2.height;
  }
  
  /**
   * Compare two element properties objects for equality
   */
  private arePropertiesEqual(props1: DiagramElementProperties, props2: DiagramElementProperties): boolean {
    // Compare the most commonly changed properties first for early exit
    if (props1.text !== props2.text) return false;
    if (props1.backgroundColor !== props2.backgroundColor) return false;
    if (props1.borderColor !== props2.borderColor) return false;
    if (props1.color !== props2.color) return false;
    
    // For other properties, check if the keys match
    const keys1 = Object.keys(props1);
    const keys2 = Object.keys(props2);
    
    if (keys1.length !== keys2.length) return false;
    
    // Check values of remaining properties
    for (const key of keys1) {
      // Skip the properties we already checked
      if (key === 'text' || key === 'backgroundColor' || 
          key === 'borderColor' || key === 'color') {
        continue;
      }
      
      if (props1[key] !== props2[key]) return false;
    }
    
    return true;
  }
  
  /**
   * Handle element selection from UI
   */
  onElementSelect(event: { getProperty: (prop: string) => any }): void {
    if (!this.graph) return;
    
    const cell = event.getProperty('cell');
    if (cell && cell.id) {
      this.diagramState.selectElement(cell.id);
    } else {
      this.diagramState.selectElement(null);
    }
  }
}
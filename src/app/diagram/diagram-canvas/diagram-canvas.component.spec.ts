import { ComponentFixture, TestBed, fakeAsync, tick } from '@angular/core/testing';
import { DiagramCanvasComponent } from './diagram-canvas.component';
import { DiagramService } from '../services/diagram.service';
import { DiagramRendererService } from '../services/diagram-renderer.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { Store } from '@ngrx/store';
import { of } from 'rxjs';
import { DiagramElementType, DiagramGraph, GraphView, GraphModel, GraphCell } from '../store/models/diagram.model';

// Define test-specific interfaces for mocking MxGraph
interface MxGraph extends DiagramGraph {
  // Additional MxGraph specific methods used by the DiagramService but not in DiagramGraph
  getStylesheet: () => any;
  refresh: () => void;
  setCellsLocked: (locked: boolean) => void;
  setAllowDanglingEdges: (allow: boolean) => void;
}

describe('DiagramCanvasComponent', () => {
  let component: DiagramCanvasComponent;
  let fixture: ComponentFixture<DiagramCanvasComponent>;
  let mockStore: jasmine.SpyObj<Store>;
  let mockDiagramService: jasmine.SpyObj<DiagramService>;
  let mockDiagramRenderer: jasmine.SpyObj<DiagramRendererService>;
  let mockLoggerService: jasmine.SpyObj<LoggerService>;
  
  // Mock graph for testing with proper interface
  const mockGraph: MxGraph = {
    // DiagramGraph methods
    getView: () => ({ 
      getScale: () => 1,
      setScale: () => {},
      refresh: () => {} 
    }),
    getModel: () => ({
      beginUpdate: jasmine.createSpy('beginUpdate'),
      endUpdate: jasmine.createSpy('endUpdate'),
      getCell: jasmine.createSpy('getCell'),
      setValue: jasmine.createSpy('setValue'),
      setStyle: jasmine.createSpy('setStyle'),
      setGeometry: jasmine.createSpy('setGeometry'),
      getChildCells: jasmine.createSpy('getChildCells'),
      getChildCount: jasmine.createSpy('getChildCount')
    }),
    getDefaultParent: jasmine.createSpy('getDefaultParent'),
    getChildCells: jasmine.createSpy('getChildCells').and.returnValue([]),
    isGridEnabled: jasmine.createSpy('isGridEnabled').and.returnValue(true),
    setGridEnabled: jasmine.createSpy('setGridEnabled'),
    zoomTo: jasmine.createSpy('zoomTo'),
    clearSelection: jasmine.createSpy('clearSelection'),
    setSelectionCells: jasmine.createSpy('setSelectionCells'),
    removeCells: jasmine.createSpy('removeCells'),
    insertVertex: jasmine.createSpy('insertVertex'),
    insertEdge: jasmine.createSpy('insertEdge'),
    sizeDidChange: jasmine.createSpy('sizeDidChange'),
    container: { style: { backgroundColor: '#ffffff' } },
    gridSize: 20,
    
    // MxGraph additional methods
    getStylesheet: jasmine.createSpy('getStylesheet'),
    refresh: jasmine.createSpy('refresh'),
    setCellsLocked: jasmine.createSpy('setCellsLocked'),
    setAllowDanglingEdges: jasmine.createSpy('setAllowDanglingEdges')
  };
  
  // Mock canvas state
  const mockCanvasState = {
    elements: [
      {
        id: '1',
        type: DiagramElementType.RECTANGLE,
        position: { x: 100, y: 100 },
        size: { width: 120, height: 60 },
        properties: {
          text: 'Test Rectangle',
          backgroundColor: '#ffffff',
          borderColor: '#000000'
        },
        zIndex: 1
      }
    ],
    zoomLevel: 1,
    showGrid: true,
    gridSize: 20,
    selectedElementIds: [],
    backgroundColor: '#ffffff'
  };

  beforeEach(async () => {
    // Create spies for dependencies
    mockStore = jasmine.createSpyObj('Store', ['select', 'dispatch']);
    mockDiagramService = jasmine.createSpyObj('DiagramService', ['initGraph']);
    mockDiagramRenderer = jasmine.createSpyObj('DiagramRendererService', ['updateGraph']);
    mockLoggerService = jasmine.createSpyObj('LoggerService', ['info', 'debug', 'error']);
    
    // Configure mock return values
    mockStore.select.and.returnValue(of(mockCanvasState));
    mockDiagramService.initGraph.and.returnValue(mockGraph);
    
    await TestBed.configureTestingModule({
      imports: [DiagramCanvasComponent],
      providers: [
        { provide: Store, useValue: mockStore },
        { provide: DiagramService, useValue: mockDiagramService },
        { provide: DiagramRendererService, useValue: mockDiagramRenderer },
        { provide: LoggerService, useValue: mockLoggerService }
      ]
    }).compileComponents();

    fixture = TestBed.createComponent(DiagramCanvasComponent);
    component = fixture.componentInstance;
  });

  it('should create', () => {
    fixture.detectChanges();
    expect(component).toBeTruthy();
  });
  
  it('should initialize diagram on AfterViewInit', () => {
    // Set up the needed ElementRef
    Object.defineProperty(component, 'diagramContainer', {
      value: { nativeElement: document.createElement('div') },
      writable: true
    });
    
    fixture.detectChanges();
    
    // Trigger ngAfterViewInit manually
    component.ngAfterViewInit();
    
    expect(mockDiagramService.initGraph).toHaveBeenCalled();
    expect(mockLoggerService.info).toHaveBeenCalledWith('Diagram canvas initialized', 'DiagramCanvasComponent');
  });
  
  it('should subscribe to canvas state changes', () => {
    fixture.detectChanges();
    
    // Check that the store selectors were called
    expect(mockStore.select).toHaveBeenCalled();
  });
  
  it('should clean up when destroyed', () => {
    // Create a spy on ResizeObserver.disconnect
    const mockResizeObserver = { disconnect: jasmine.createSpy('disconnect') };
    Object.defineProperty(component, 'resizeObserver', {
      value: mockResizeObserver,
      writable: true
    });
    
    fixture.detectChanges();
    component.ngOnDestroy();
    
    // Check that resize observer was disconnected
    expect(mockResizeObserver.disconnect).toHaveBeenCalled();
  });
  
  // Performance test - check that the memoization works
  it('should not update graph when state has not changed', fakeAsync(() => {
    // Initialize component
    Object.defineProperty(component, 'diagramContainer', {
      value: { nativeElement: document.createElement('div') },
      writable: true
    });
    fixture.detectChanges();
    component.ngAfterViewInit();
    
    // Reset the spy count
    mockDiagramRenderer.updateGraph.calls.reset();
    
    // Push the same state twice
    component['updateDiagramFromState'](mockCanvasState);
    component['updateDiagramFromState'](mockCanvasState);
    
    // Should only call updateGraph once because the state hasn't changed
    expect(mockDiagramRenderer.updateGraph).toHaveBeenCalledTimes(1);
    
    // Change a property in the state
    const newState = {
      ...mockCanvasState,
      zoomLevel: 1.5
    };
    
    // Now it should call updateGraph again
    component['updateDiagramFromState'](newState);
    expect(mockDiagramRenderer.updateGraph).toHaveBeenCalledTimes(2);
  }));
  
  it('should detect element changes correctly in deep comparison', () => {
    // Test equality check when positions are the same
    const pos1 = { x: 100, y: 200 };
    const pos2 = { x: 100, y: 200 };
    expect(component['arePositionsEqual'](pos1, pos2)).toBeTrue();
    
    // Test inequality when positions differ
    const pos3 = { x: 150, y: 200 };
    expect(component['arePositionsEqual'](pos1, pos3)).toBeFalse();
    
    // Test size equality
    const size1 = { width: 100, height: 50 };
    const size2 = { width: 100, height: 50 };
    expect(component['areSizesEqual'](size1, size2)).toBeTrue();
    
    // Test size inequality
    const size3 = { width: 120, height: 50 };
    expect(component['areSizesEqual'](size1, size3)).toBeFalse();
    
    // Test properties equality
    const props1 = { text: 'Hello', backgroundColor: '#fff', borderColor: '#000', color: '#333' };
    const props2 = { text: 'Hello', backgroundColor: '#fff', borderColor: '#000', color: '#333' };
    expect(component['arePropertiesEqual'](props1, props2)).toBeTrue();
    
    // Test properties inequality - different text
    const props3 = { text: 'Hello World', backgroundColor: '#fff', borderColor: '#000', color: '#333' };
    expect(component['arePropertiesEqual'](props1, props3)).toBeFalse();
    
    // Test properties inequality - different color
    const props4 = { text: 'Hello', backgroundColor: '#ccc', borderColor: '#000', color: '#333' };
    expect(component['arePropertiesEqual'](props1, props4)).toBeFalse();
  });
  
  it('should detect element changes in arrays using areElementsChanged', () => {
    // Create test elements
    const element1 = {
      id: '1',
      type: DiagramElementType.RECTANGLE,
      position: { x: 100, y: 100 },
      size: { width: 120, height: 60 },
      properties: {
        text: 'Test Rectangle',
        backgroundColor: '#ffffff',
        borderColor: '#000000'
      },
      zIndex: 1
    };
    
    const element2 = {
      id: '2',
      type: DiagramElementType.CIRCLE,
      position: { x: 200, y: 200 },
      size: { width: 80, height: 80 },
      properties: {
        text: 'Test Circle',
        backgroundColor: '#f0f0f0',
        borderColor: '#000000'
      },
      zIndex: 2
    };
    
    // Test with identical arrays
    const array1 = [element1, element2];
    const array2 = [element1, element2];
    expect(component['areElementsChanged'](array1, array2)).toBeFalse();
    
    // Test with different length arrays
    const array3 = [element1];
    expect(component['areElementsChanged'](array1, array3)).toBeTrue();
    
    // Test with same elements but different order
    const array4 = [element2, element1];
    expect(component['areElementsChanged'](array1, array4)).toBeFalse();
    
    // Test with a modified element
    const modifiedElement = {
      ...element1,
      position: { x: 150, y: 100 }
    };
    const array5 = [modifiedElement, element2];
    expect(component['areElementsChanged'](array1, array5)).toBeTrue();
    
    // Test with a modified nested property
    const elementWithModifiedProps = {
      ...element1,
      properties: {
        ...element1.properties,
        text: 'Modified Text'
      }
    };
    const array6 = [elementWithModifiedProps, element2];
    expect(component['areElementsChanged'](array1, array6)).toBeTrue();
  });
  
  it('should handle cleanup properly on destroy', () => {
    // Create spy on Subject methods
    spyOn(component['destroy$'], 'next');
    spyOn(component['destroy$'], 'complete');
    
    // Set up mock resizeObserver
    const mockResizeObserver = { disconnect: jasmine.createSpy('disconnect') };
    component['resizeObserver'] = mockResizeObserver as any;
    
    // Call destroy
    component.ngOnDestroy();
    
    // Check that cleanup was done correctly
    expect(component['destroy$'].next).toHaveBeenCalled();
    expect(component['destroy$'].complete).toHaveBeenCalled();
    expect(mockResizeObserver.disconnect).toHaveBeenCalled();
  });
});
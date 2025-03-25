import { TestBed } from '@angular/core/testing';
import { DiagramRendererService, DiagramGraph } from './diagram-renderer.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { DiagramElement, DiagramElementType } from '../store/models/diagram.model';

describe('DiagramRendererService', () => {
  let service: DiagramRendererService;
  let loggerSpy: jasmine.SpyObj<LoggerService>;
  let mockGraph: jasmine.SpyObj<DiagramGraph>;
  let mockModel: any;
  let mockView: any;

  beforeEach(() => {
    loggerSpy = jasmine.createSpyObj('LoggerService', ['debug', 'info', 'error', 'warn']);
    mockModel = jasmine.createSpyObj('Model', ['beginUpdate', 'endUpdate', 'getCell', 'setValue', 'setStyle', 'setGeometry']);
    mockView = jasmine.createSpyObj('View', ['getScale', 'setScale', 'refresh']);
    
    // Setup view returns
    mockView.getScale.and.returnValue(1);
    
    mockGraph = jasmine.createSpyObj('DiagramGraph', [
      'getView', 'getModel', 'getDefaultParent', 'getChildCells',
      'isGridEnabled', 'setGridEnabled', 'zoomTo', 'clearSelection',
      'setSelectionCells', 'removeCells', 'insertVertex', 'insertEdge',
      'sizeDidChange'
    ]);
    
    // Setup mockGraph properties and returns
    mockGraph.getModel.and.returnValue(mockModel);
    mockGraph.getView.and.returnValue(mockView);
    mockGraph.getDefaultParent.and.returnValue({ id: 'parent1' });
    mockGraph.getChildCells.and.returnValue([]);
    mockGraph.isGridEnabled.and.returnValue(true);
    mockGraph.container = document.createElement('div');
    mockGraph.gridSize = 20;
    mockGraph.container.style.backgroundColor = '#ffffff';
    
    TestBed.configureTestingModule({
      providers: [
        DiagramRendererService,
        { provide: LoggerService, useValue: loggerSpy }
      ]
    });
    
    service = TestBed.inject(DiagramRendererService);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  describe('updateGraph', () => {
    it('should update graph settings based on provided values', () => {
      const settings = {
        zoomLevel: 1.5,
        showGrid: true,
        gridSize: 30,
        backgroundColor: '#f0f0f0',
        selectedElementIds: ['elem1', 'elem2']
      };
      
      // Mock model.getCell to return cells for selection
      mockModel.getCell.and.callFake((id: string) => {
        return { id, getId: () => id };
      });
      
      service.updateGraph(mockGraph, [], settings);
      
      expect(mockGraph.zoomTo).toHaveBeenCalledWith(1.5, true);
      expect(mockGraph.setGridEnabled).toHaveBeenCalledWith(true);
      expect(mockGraph.gridSize).toBe(30);
      expect(mockGraph.container.style.backgroundColor).toBe('#f0f0f0');
      expect(mockGraph.clearSelection).toHaveBeenCalled();
      expect(mockGraph.setSelectionCells).toHaveBeenCalled();
    });
    
    it('should handle empty arrays and null values safely', () => {
      const settings = {
        zoomLevel: 1,
        showGrid: false,
        gridSize: 10,
        backgroundColor: '#ffffff',
        selectedElementIds: []
      };
      
      service.updateGraph(mockGraph, [], settings);
      
      expect(mockGraph.clearSelection).toHaveBeenCalled();
      expect(mockGraph.setSelectionCells).not.toHaveBeenCalled();
    });
    
    it('should not make unnecessary updates', () => {
      // Skip unnecessary updates
      mockGraph.isGridEnabled.and.returnValue(true);
      mockView.getScale.and.returnValue(1);
      
      const settings = {
        zoomLevel: 1,
        showGrid: true,
        gridSize: 20,
        backgroundColor: '#ffffff',
        selectedElementIds: []
      };
      
      service.updateGraph(mockGraph, [], settings);
      
      expect(mockGraph.zoomTo).not.toHaveBeenCalled();
      expect(mockGraph.setGridEnabled).not.toHaveBeenCalled();
    });
    
    it('should handle error gracefully', () => {
      const settings = {
        zoomLevel: 1,
        showGrid: true,
        gridSize: 20,
        backgroundColor: '#ffffff',
        selectedElementIds: []
      };
      
      // Force an error
      mockGraph.getView.and.throwError('Test error');
      
      service.updateGraph(mockGraph, [], settings);
      
      expect(loggerSpy.error).toHaveBeenCalled();
    });
  });

  describe('element manipulation', () => {
    it('should handle adding new elements', () => {
      const elements = [
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
      ];
      
      const settings = {
        zoomLevel: 1,
        showGrid: true,
        gridSize: 20,
        backgroundColor: '#ffffff',
        selectedElementIds: []
      };
      
      service.updateGraph(mockGraph, elements, settings);
      
      expect(mockModel.beginUpdate).toHaveBeenCalled();
      expect(mockGraph.insertVertex).toHaveBeenCalled();
      expect(mockModel.endUpdate).toHaveBeenCalled();
    });
    
    it('should handle connector elements', () => {
      // Mock source and target cells
      mockModel.getCell.and.callFake((id: string) => {
        if (id === 'source1' || id === 'target1') {
          return { id, getId: () => id };
        }
        return null;
      });
      
      const elements = [
        {
          id: 'conn1',
          type: DiagramElementType.CONNECTOR,
          position: { x: 0, y: 0 },
          size: { width: 0, height: 0 },
          properties: {
            text: 'Test Connection',
            sourceElementId: 'source1',
            targetElementId: 'target1'
          },
          zIndex: 1
        }
      ];
      
      const settings = {
        zoomLevel: 1,
        showGrid: true,
        gridSize: 20,
        backgroundColor: '#ffffff',
        selectedElementIds: []
      };
      
      service.updateGraph(mockGraph, elements, settings);
      
      expect(mockModel.beginUpdate).toHaveBeenCalled();
      expect(mockGraph.insertEdge).toHaveBeenCalled();
      expect(mockModel.endUpdate).toHaveBeenCalled();
    });
  });
});
import { TestBed } from '@angular/core/testing';
import { DiagramService } from './diagram.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { StorageService } from '../../shared/services/storage/storage.service';
import { BehaviorSubject } from 'rxjs';
import { StorageFile } from '../../shared/services/storage/providers/storage-provider.interface';
import { DiagramCell, DiagramData } from './diagram-service';

describe('DiagramService', () => {
  let service: DiagramService;
  let loggerSpy: jasmine.SpyObj<LoggerService>;
  let storageSpy: jasmine.SpyObj<StorageService>;
  let mockGraph: any;
  let mockModel: any;
  let mockContainer: HTMLElement;

  const mockStorageFile: StorageFile = {
    id: 'file-id-123',
    name: 'Test Diagram.json',
    mimeType: 'application/json',
    size: 1024
  };

  beforeEach(() => {
    // Create spy objects for dependencies
    loggerSpy = jasmine.createSpyObj('LoggerService', ['debug', 'info', 'error', 'warn']);
    storageSpy = jasmine.createSpyObj('StorageService', ['saveFile', 'createFile', 'loadFile', 'listFiles', 'initialize']);
    
    // Mock storage service methods
    storageSpy.saveFile.and.returnValue(Promise.resolve());
    storageSpy.createFile.and.returnValue(Promise.resolve(mockStorageFile));
    storageSpy.loadFile.and.returnValue(Promise.resolve('{"cells":[], "title":"Test Diagram"}'));
    storageSpy.listFiles.and.returnValue(Promise.resolve([mockStorageFile]));
    storageSpy.initialize.and.returnValue(Promise.resolve());
    
    // Create mock container
    mockContainer = document.createElement('div');
    
    // Mock Graph components from @maxgraph/core
    mockModel = jasmine.createSpyObj('Model', ['beginUpdate', 'endUpdate', 'getCell', 'setValue', 'setStyle', 'setGeometry']);
    mockGraph = jasmine.createSpyObj('Graph', [
      'getStylesheet', 'getModel', 'removeCells', 'getChildCells', 
      'getDefaultParent', 'insertVertex', 'insertEdge', 'setCellsLocked',
      'setAllowDanglingEdges', 'setConnectable', 'setCellsMovable', 
      'setCellsResizable', 'setCellsResizable', 'setGridEnabled',
      'getSelectionCells'
    ]);
    
    // Set up mock returns
    mockGraph.getModel.and.returnValue(mockModel);
    mockGraph.getChildCells.and.returnValue([]);
    mockGraph.getDefaultParent.and.returnValue({});
    mockGraph.getStylesheet.and.returnValue({
      getDefaultVertexStyle: () => ({}),
      getDefaultEdgeStyle: () => ({})
    });
    
    // Set up TestBed
    TestBed.configureTestingModule({
      providers: [
        DiagramService,
        { provide: LoggerService, useValue: loggerSpy },
        { provide: StorageService, useValue: storageSpy }
      ]
    });
    
    service = TestBed.inject(DiagramService);
    
    // Create a spy for the Graph constructor from @maxgraph/core
    spyOn(service as any, 'configureGraph').and.callThrough();
    spyOn(service as any, 'registerCustomShapes').and.callThrough();
    
    // Mock the @maxgraph/core Graph class
    (window as any).mockMaxGraph = {
      Graph: jasmine.createSpy('Graph').and.returnValue(mockGraph)
    };
    
    // Replace the constructor in service with our mock
    spyOn(global, 'Graph').and.returnValue(mockGraph);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  describe('initGraph', () => {
    it('should initialize a graph with the provided container', () => {
      const result = service.initGraph(mockContainer);
      
      expect(Graph).toHaveBeenCalledWith(mockContainer);
      expect((service as any).configureGraph).toHaveBeenCalledWith(mockGraph);
      expect(mockGraph.setConnectable).toHaveBeenCalledWith(true);
      expect(result).toBe(mockGraph);
    });
    
    it('should handle initialization errors', () => {
      // Simulate an error during initialization
      (Graph as jasmine.Spy).and.throwError('Test error');
      
      expect(() => service.initGraph(mockContainer)).toThrowError();
      expect(loggerSpy.error).toHaveBeenCalled();
    });
  });

  describe('resetDiagram', () => {
    beforeEach(() => {
      // Initialize graph first
      (service as any).graph = mockGraph;
    });
    
    it('should reset the diagram to a blank state', () => {
      service.resetDiagram();
      
      expect(mockModel.beginUpdate).toHaveBeenCalled();
      expect(mockGraph.removeCells).toHaveBeenCalled();
      expect(mockModel.endUpdate).toHaveBeenCalled();
      expect(service.getCurrentDiagram()).toBeTruthy();
      expect(service.getCurrentDiagram()?.title).toBe('Untitled Diagram');
    });
  });

  describe('addNode', () => {
    beforeEach(() => {
      // Initialize graph first
      (service as any).graph = mockGraph;
      mockGraph.insertVertex.and.returnValue({ getValue: () => 'Test Node' });
    });
    
    it('should add a node to the graph', () => {
      const result = service.addNode(10, 20, 100, 50, 'Test Node');
      
      expect(mockModel.beginUpdate).toHaveBeenCalled();
      expect(mockGraph.insertVertex).toHaveBeenCalledWith(
        jasmine.any(Object), null, 'Test Node', 10, 20, 100, 50, undefined
      );
      expect(mockModel.endUpdate).toHaveBeenCalled();
      expect(service.isDiagramDirty()).toBe(true);
    });
    
    it('should throw error if graph is not initialized', () => {
      // Reset the service to ensure graph is null
      (service as any).graph = null;
      
      expect(() => service.addNode(10, 20, 100, 50, 'Test Node')).toThrowError('Graph not initialized');
    });
  });

  describe('addEdge', () => {
    let mockSource: any;
    let mockTarget: any;
    
    beforeEach(() => {
      // Initialize graph first
      (service as any).graph = mockGraph;
      mockSource = { getValue: () => 'Source' };
      mockTarget = { getValue: () => 'Target' };
      mockGraph.insertEdge.and.returnValue({ getValue: () => 'Test Edge' });
    });
    
    it('should add an edge between two nodes', () => {
      const result = service.addEdge(mockSource, mockTarget, 'Test Edge');
      
      expect(mockModel.beginUpdate).toHaveBeenCalled();
      expect(mockGraph.insertEdge).toHaveBeenCalledWith(
        jasmine.any(Object), null, 'Test Edge', mockSource, mockTarget, undefined
      );
      expect(mockModel.endUpdate).toHaveBeenCalled();
      expect(service.isDiagramDirty()).toBe(true);
    });
  });

  describe('deleteSelected', () => {
    beforeEach(() => {
      // Initialize graph first
      (service as any).graph = mockGraph;
      mockGraph.getSelectionCells.and.returnValue([{ getId: () => 'cell1' }, { getId: () => 'cell2' }]);
    });
    
    it('should delete selected cells', () => {
      service.deleteSelected();
      
      expect(mockGraph.getSelectionCells).toHaveBeenCalled();
      expect(mockModel.beginUpdate).toHaveBeenCalled();
      expect(mockGraph.removeCells).toHaveBeenCalled();
      expect(mockModel.endUpdate).toHaveBeenCalled();
      expect(service.isDiagramDirty()).toBe(true);
    });
    
    it('should do nothing if no cells are selected', () => {
      mockGraph.getSelectionCells.and.returnValue([]);
      
      service.deleteSelected();
      
      expect(mockModel.beginUpdate).not.toHaveBeenCalled();
      expect(mockGraph.removeCells).not.toHaveBeenCalled();
    });
  });

  describe('saveDiagram', () => {
    beforeEach(() => {
      // Initialize graph first
      (service as any).graph = mockGraph;
      
      // Mock exportDiagram to return some diagram data
      spyOn(service, 'exportDiagram').and.returnValue({
        cells: [],
        title: 'Test Diagram',
        updatedAt: new Date().toISOString()
      } as DiagramData);
    });
    
    it('should save a new diagram', async () => {
      const result = await service.saveDiagram('New Diagram.json');
      
      expect(service.exportDiagram).toHaveBeenCalled();
      expect(storageSpy.createFile).toHaveBeenCalledWith(
        'New Diagram.json', jasmine.any(String)
      );
      expect(result).toEqual(mockStorageFile);
      expect(service.isDiagramDirty()).toBe(false);
    });
    
    it('should update an existing diagram', async () => {
      // Set current file
      (service as any).currentFile = new BehaviorSubject(mockStorageFile);
      
      const result = await service.saveDiagram();
      
      expect(service.exportDiagram).toHaveBeenCalled();
      expect(storageSpy.saveFile).toHaveBeenCalledWith(
        mockStorageFile.id, jasmine.any(String)
      );
      expect(result).toEqual(mockStorageFile);
      expect(service.isDiagramDirty()).toBe(false);
    });
    
    it('should handle save errors', async () => {
      storageSpy.createFile.and.rejectWith(new Error('Save error'));
      
      await expectAsync(service.saveDiagram()).toBeRejected();
      expect(loggerSpy.error).toHaveBeenCalled();
    });
  });

  describe('loadDiagram', () => {
    beforeEach(() => {
      // Initialize graph first
      (service as any).graph = mockGraph;
      
      // Mock importDiagram
      spyOn(service, 'importDiagram');
    });
    
    it('should load a diagram from storage', async () => {
      await service.loadDiagram('file-id-123');
      
      expect(storageSpy.loadFile).toHaveBeenCalledWith('file-id-123');
      expect(service.importDiagram).toHaveBeenCalledWith(jasmine.any(Object));
      expect(storageSpy.listFiles).toHaveBeenCalled();
    });
    
    it('should handle load errors', async () => {
      storageSpy.loadFile.and.rejectWith(new Error('Load error'));
      
      await expectAsync(service.loadDiagram('file-id-123')).toBeRejected();
      expect(loggerSpy.error).toHaveBeenCalled();
    });
  });

  describe('isDiagramDirty', () => {
    it('should return the current dirty state', () => {
      expect(service.isDiagramDirty()).toBe(false);
      
      // Set dirty state to true
      service.markDiagramDirty();
      
      expect(service.isDiagramDirty()).toBe(true);
    });
  });

  describe('observable properties', () => {
    it('should expose observables for state changes', () => {
      expect(service.currentDiagram$).toBeTruthy();
      expect(service.isDirty$).toBeTruthy();
      expect(service.currentFile$).toBeTruthy();
    });
  });
});
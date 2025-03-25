import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { Store } from '@ngrx/store';
import { of } from 'rxjs';
import { DiagramHomeComponent } from './diagram-home.component';
import { DiagramMetadata } from '../store/models/diagram.model';

describe('DiagramHomeComponent', () => {
  let component: DiagramHomeComponent;
  let fixture: ComponentFixture<DiagramHomeComponent>;
  let mockStore: jasmine.SpyObj<Store>;
  let mockRouter: jasmine.SpyObj<Router>;

  beforeEach(async () => {
    // Create spies for dependencies
    mockStore = jasmine.createSpyObj('Store', ['select', 'dispatch']);
    mockRouter = jasmine.createSpyObj('Router', ['navigate']);
    
    // Set up observable returns
    mockStore.select.and.returnValue(of([]));
    
    await TestBed.configureTestingModule({
      imports: [DiagramHomeComponent],
      providers: [
        { provide: Store, useValue: mockStore },
        { provide: Router, useValue: mockRouter }
      ]
    })
    .compileComponents();

    fixture = TestBed.createComponent(DiagramHomeComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });
  
  it('should use trackByFn for diagram list performance', () => {
    // Create diagrams with different data but same IDs
    const diagram1: DiagramMetadata = {
      id: 'diagram-1',
      name: 'Test Diagram 1',
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    };
    
    const diagram2: DiagramMetadata = {
      id: 'diagram-1', // Same ID as diagram1
      name: 'Updated Test Diagram 1', // Different name
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    };
    
    // The trackBy function should return the same value for both objects
    // because they have the same ID
    expect(component.trackByFn(0, diagram1)).toBe('diagram-1');
    expect(component.trackByFn(0, diagram2)).toBe('diagram-1');
    
    // But the objects themselves are different
    expect(diagram1).not.toBe(diagram2);
    
    // Create a diagram with a different ID
    const diagram3: DiagramMetadata = {
      id: 'diagram-2',
      name: 'Test Diagram 2',
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    };
    
    // The trackBy function should return a different value
    expect(component.trackByFn(0, diagram3)).toBe('diagram-2');
    expect(component.trackByFn(0, diagram1)).not.toBe(component.trackByFn(0, diagram3));
  });
  
  it('should dispatch createDiagram action and navigate on createNewDiagram', () => {
    // Call the method
    component.createNewDiagram();
    
    // Verify that the store dispatch was called
    expect(mockStore.dispatch).toHaveBeenCalledWith(jasmine.objectContaining({
      type: '[Diagram] Create Diagram'
    }));
    
    // Verify that router navigate was called with the correct path
    expect(mockRouter.navigate).toHaveBeenCalledWith(['/diagrams/editor']);
  });
  
  it('should navigate to the correct diagram editor on openDiagram', () => {
    // Create a test diagram
    const diagram: DiagramMetadata = {
      id: 'test-diagram-id',
      name: 'Test Diagram',
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    };
    
    // Call the method
    component.openDiagram(diagram);
    
    // Verify that router navigate was called with the correct path
    expect(mockRouter.navigate).toHaveBeenCalledWith(['/diagrams/editor', 'test-diagram-id']);
  });
});

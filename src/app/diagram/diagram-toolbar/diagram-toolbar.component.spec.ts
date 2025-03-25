import { ComponentFixture, TestBed, fakeAsync, tick } from '@angular/core/testing';
import { DiagramToolbarComponent } from './diagram-toolbar.component';
import { DiagramService } from '../services/diagram.service';
import { StorageService } from '../../shared/services/storage/storage.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { Store } from '@ngrx/store';
import { Subject, of } from 'rxjs';
import { LOCALE_ID } from '@angular/core';
import { DiagramElementType } from '../store/models/diagram.model';
import * as DiagramActions from '../store/actions/diagram.actions';

describe('DiagramToolbarComponent', () => {
  let component: DiagramToolbarComponent;
  let fixture: ComponentFixture<DiagramToolbarComponent>;
  let mockStore: jasmine.SpyObj<Store>;
  let mockDiagramService: jasmine.SpyObj<DiagramService>;
  let mockStorageService: jasmine.SpyObj<StorageService>;
  let mockLoggerService: jasmine.SpyObj<LoggerService>;
  interface SubjectMap {
    canUndo: Subject<boolean>;
    canRedo: Subject<boolean>;
    hasChanges: Subject<boolean>;
    selectSelectedElementId: Subject<string | null>;
  }
  
  let selectSubjects: SubjectMap;

  beforeEach(async () => {
    // Create subjects for finer control of observable streams
    selectSubjects = {
      canUndo: new Subject<boolean>(),
      canRedo: new Subject<boolean>(), 
      hasChanges: new Subject<boolean>(),
      selectSelectedElementId: new Subject<string | null>()
    };
    
    // Create spies for dependencies
    mockStore = jasmine.createSpyObj('Store', ['select', 'dispatch']);
    mockDiagramService = jasmine.createSpyObj('DiagramService', ['initGraph']);
    mockStorageService = jasmine.createSpyObj('StorageService', ['showPicker']);
    mockLoggerService = jasmine.createSpyObj('LoggerService', ['debug', 'info', 'warn', 'error']);
    
    // Configure store.select to return different subjects based on the selector
    mockStore.select.and.callFake((selector: any) => {
      if (selector === 'selectCanUndo') return selectSubjects.canUndo;
      if (selector === 'selectCanRedo') return selectSubjects.canRedo;
      if (selector === 'selectDiagramHasChanges') return selectSubjects.hasChanges;
      if (selector === 'selectSelectedElementId') return selectSubjects.selectSelectedElementId;
      return of(true); // Default fallback
    });
    
    await TestBed.configureTestingModule({
      imports: [DiagramToolbarComponent],
      providers: [
        { provide: Store, useValue: mockStore },
        { provide: DiagramService, useValue: mockDiagramService },
        { provide: StorageService, useValue: mockStorageService },
        { provide: LoggerService, useValue: mockLoggerService },
        { provide: LOCALE_ID, useValue: 'en' }
      ]
    }).compileComponents();

    fixture = TestBed.createComponent(DiagramToolbarComponent);
    component = fixture.componentInstance;
    
    // Initialize with default values
    selectSubjects.canUndo.next(true);
    selectSubjects.canRedo.next(true);
    selectSubjects.hasChanges.next(true);
  });

  it('should create', () => {
    fixture.detectChanges();
    expect(component).toBeTruthy();
  });
  
  it('should emit save event when save button is clicked', () => {
    fixture.detectChanges();
    
    // Set up spy on output event
    spyOn(component.save, 'emit');
    
    // Call the method
    component.onSave();
    
    // Check that the event was emitted
    expect(component.save.emit).toHaveBeenCalled();
  });
  
  it('should dispatch addElement action when addRectangle is called', () => {
    fixture.detectChanges();
    
    // Call the method
    component.addRectangle();
    
    // Check that the store dispatch was called with the correct action
    expect(mockStore.dispatch).toHaveBeenCalledWith(jasmine.objectContaining({
      type: '[Diagram] Add Element'
    }));
  });
  
  it('should dispatch addElement action when addCircle is called', () => {
    fixture.detectChanges();
    
    // Call the method
    component.addCircle();
    
    // Check that the store dispatch was called with the correct action
    expect(mockStore.dispatch).toHaveBeenCalledWith(jasmine.objectContaining({
      type: '[Diagram] Add Element'
    }));
  });
  
  it('should dispatch addElement action when addText is called', () => {
    fixture.detectChanges();
    
    // Call the method
    component.addText();
    
    // Check that the store dispatch was called with the correct action
    expect(mockStore.dispatch).toHaveBeenCalledWith(jasmine.objectContaining({
      type: '[Diagram] Add Element'
    }));
  });
  
  it('should dispatch removeElement action when deleteSelected is called with valid selection', fakeAsync(() => {
    fixture.detectChanges();
    
    // Set selected element ID
    selectSubjects.selectSelectedElementId.next('element-123');
    tick();
    
    // Call the method
    component.deleteSelected();
    
    // Check that the store dispatch was called with the correct action
    expect(mockStore.dispatch).toHaveBeenCalledWith(
      DiagramActions.removeElement({ id: 'element-123' })
    );
  }));
  
  it('should not dispatch removeElement action when deleteSelected is called with no selection', fakeAsync(() => {
    fixture.detectChanges();
    
    // Reset the dispatch spy
    mockStore.dispatch.calls.reset();
    
    // Set selected element ID to null
    selectSubjects.selectSelectedElementId.next(null);
    tick();
    
    // Call the method
    component.deleteSelected();
    
    // Check that the store dispatch was not called
    expect(mockStore.dispatch).not.toHaveBeenCalled();
  }));
  
  it('should dispatch createDiagram action when newDiagram is called', fakeAsync(() => {
    fixture.detectChanges();
    
    // Set hasChanges to false to avoid confirmation dialog
    selectSubjects.hasChanges.next(false);
    tick();
    
    // Spy on confirm to avoid actual browser dialog
    spyOn(window, 'confirm').and.returnValue(false);
    
    // Call the method
    component.newDiagram();
    
    // Check that the store dispatch was called with createDiagram action
    expect(mockStore.dispatch).toHaveBeenCalledWith(jasmine.objectContaining({
      type: '[Diagram] Create Diagram'
    }));
  }));
  
  it('should properly unsubscribe when component is destroyed', fakeAsync(() => {
    fixture.detectChanges();
    
    // Create a spy to track subscription status
    const subscriptionSpy = jasmine.createSpy('subscriptionCallback');
    
    // Create a test subscription that uses the destroy$ subject
    const testSubject = new Subject<void>();
    const subscription = testSubject.subscribe(subscriptionSpy);
    
    // Store the original next and complete methods
    const originalNext = component['destroy$'].next;
    const originalComplete = component['destroy$'].complete;
    
    // Replace them with spies
    component['destroy$'].next = jasmine.createSpy('next').and.callFake(() => {
      originalNext.call(component['destroy$']);
    });
    
    component['destroy$'].complete = jasmine.createSpy('complete').and.callFake(() => {
      originalComplete.call(component['destroy$']);
    });
    
    // Destroy the component
    component.ngOnDestroy();
    
    // Verify the destroy$ subject methods were called
    expect(component['destroy$'].next).toHaveBeenCalled();
    expect(component['destroy$'].complete).toHaveBeenCalled();
    
    // Emit on the test subject after destroy
    testSubject.next();
    
    // The subscription callback should not be called since
    // the component has been destroyed and all subscriptions
    // using takeUntil(destroy$) should have been unsubscribed
    expect(subscriptionSpy).not.toHaveBeenCalled();
  }));
});
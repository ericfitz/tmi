import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, BehaviorSubject } from 'rxjs';
import { TranslateModule, TranslateLoader, TranslateService } from '@ngx-translate/core';
import { CommonModule } from '@angular/common';
import { DebugElement } from '@angular/core';
import { By } from '@angular/platform-browser';

import { DiagramToolbarComponent } from './diagram-toolbar.component';
import { DiagramService } from '../services/diagram.service';
import { StorageService } from '../../shared/services/storage/storage.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { PickerResult } from '../../shared/services/storage/providers/storage-provider.interface';

// Mock translation loader for testing
class MockTranslateLoader implements TranslateLoader {
  getTranslation(lang: string) {
    return of({
      'DIAGRAM': {
        'TOOLBAR': {
          'NEW': 'New Test',
          'OPEN': 'Open Test',
          'SAVE': 'Save Test',
          'SAVE_AS': 'Save As Test',
          'ADD_NODE': 'Add Node Test',
          'DELETE': 'Delete Test',
          'UNSAVED_CHANGES': 'Unsaved changes test {0}',
          'UNSAVED_CHANGES_CREATE': 'creating test',
          'UNSAVED_CHANGES_OPEN': 'opening test'
        },
        'PICKER': {
          'OPEN_TITLE': 'Open Diagram Test',
          'SAVE_TITLE': 'Save Diagram As Test'
        }
      }
    });
  }
}

// Mock DiagramService
class MockDiagramService {
  private _isDirty = new BehaviorSubject<boolean>(false);
  private _currentFile = new BehaviorSubject<any>(null);
  
  isDirty$ = this._isDirty.asObservable();
  currentFile$ = this._currentFile.asObservable();
  
  setDirty(isDirty: boolean) {
    this._isDirty.next(isDirty);
  }
  
  setCurrentFile(file: any) {
    this._currentFile.next(file);
  }
  
  isDiagramDirty() {
    return this._isDirty.value;
  }
  
  getCurrentFile() {
    return this._currentFile.value;
  }
  
  addNode = jasmine.createSpy('addNode');
  deleteSelected = jasmine.createSpy('deleteSelected');
  resetDiagram = jasmine.createSpy('resetDiagram');
  loadDiagram = jasmine.createSpy('loadDiagram').and.returnValue(Promise.resolve());
  saveDiagram = jasmine.createSpy('saveDiagram').and.returnValue(Promise.resolve());
}

// Mock StorageService
class MockStorageService {
  showPicker = jasmine.createSpy('showPicker').and.returnValue(Promise.resolve({
    action: 'picked',
    file: {
      id: 'test-file-id',
      name: 'Test Diagram.json'
    }
  } as PickerResult));
}

// Mock LoggerService
class MockLoggerService {
  debug() {}
  info() {}
  warn() {}
  error() {}
}

describe('DiagramToolbarComponent', () => {
  let component: DiagramToolbarComponent;
  let fixture: ComponentFixture<DiagramToolbarComponent>;
  let diagramService: MockDiagramService;
  let storageService: StorageService;
  let translate: TranslateService;
  let de: DebugElement;
  
  // Spy on window.confirm
  let confirmSpy: jasmine.Spy;

  beforeEach(async () => {
    // Create spy for confirm
    confirmSpy = spyOn(window, 'confirm').and.returnValue(true);
    
    await TestBed.configureTestingModule({
      imports: [
        CommonModule,
        DiagramToolbarComponent,
        TranslateModule.forRoot({
          loader: { provide: TranslateLoader, useClass: MockTranslateLoader }
        })
      ],
      providers: [
        { provide: DiagramService, useClass: MockDiagramService },
        { provide: StorageService, useClass: MockStorageService },
        { provide: LoggerService, useClass: MockLoggerService }
      ]
    })
    .compileComponents();

    fixture = TestBed.createComponent(DiagramToolbarComponent);
    component = fixture.componentInstance;
    diagramService = TestBed.inject(DiagramService) as unknown as MockDiagramService;
    storageService = TestBed.inject(StorageService);
    translate = TestBed.inject(TranslateService);
    de = fixture.debugElement;
    
    // Set up translation
    translate.use('en');
    
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('should display toolbar with file operation buttons', () => {
    // Get file operation buttons
    const fileButtons = de.queryAll(By.css('.file-operations button'));
    
    // Should have 4 buttons: New, Open, Save, Save As
    expect(fileButtons.length).toBe(4);
    
    // Check text content of buttons
    expect(fileButtons[0].nativeElement.textContent.trim()).toContain('New Test');
    expect(fileButtons[1].nativeElement.textContent.trim()).toContain('Open Test');
    expect(fileButtons[2].nativeElement.textContent.trim()).toContain('Save Test');
    expect(fileButtons[3].nativeElement.textContent.trim()).toContain('Save As Test');
  });

  it('should display toolbar with edit operation buttons', () => {
    // Get edit operation buttons
    const editButtons = de.queryAll(By.css('.edit-operations button'));
    
    // Should have 2 buttons: Add Node, Delete
    expect(editButtons.length).toBe(2);
    
    // Check text content of buttons
    expect(editButtons[0].nativeElement.textContent.trim()).toContain('Add Node Test');
    expect(editButtons[1].nativeElement.textContent.trim()).toContain('Delete Test');
  });

  it('should call addNode when Add Node button is clicked', () => {
    // Get Add Node button and click it
    const addNodeButton = de.queryAll(By.css('.edit-operations button'))[0];
    addNodeButton.nativeElement.click();
    
    // Verify method was called
    expect(diagramService.addNode).toHaveBeenCalled();
  });

  it('should call deleteSelected when Delete button is clicked', () => {
    // Get Delete button and click it
    const deleteButton = de.queryAll(By.css('.edit-operations button'))[1];
    deleteButton.nativeElement.click();
    
    // Verify method was called
    expect(diagramService.deleteSelected).toHaveBeenCalled();
  });

  it('should prompt to save unsaved changes when creating new diagram', () => {
    // Set diagram as dirty
    diagramService.setDirty(true);
    fixture.detectChanges();
    
    // Get New button and click it
    const newButton = de.queryAll(By.css('.file-operations button'))[0];
    newButton.nativeElement.click();
    
    // Verify confirm was called with appropriate message
    expect(confirmSpy).toHaveBeenCalled();
    
    // Verify resetDiagram was called after confirmation
    expect(diagramService.resetDiagram).toHaveBeenCalled();
  });

  it('should prompt to save unsaved changes when opening diagram', async () => {
    // Set diagram as dirty
    diagramService.setDirty(true);
    fixture.detectChanges();
    
    // Get Open button and click it
    const openButton = de.queryAll(By.css('.file-operations button'))[1];
    openButton.nativeElement.click();
    
    // Wait for async operation
    await fixture.whenStable();
    
    // Verify confirm was called with appropriate message
    expect(confirmSpy).toHaveBeenCalled();
    
    // Verify showPicker and loadDiagram were called after confirmation
    expect(storageService.showPicker).toHaveBeenCalled();
    expect(diagramService.loadDiagram).toHaveBeenCalledWith('test-file-id');
  });

  it('should save existing diagram when Save button is clicked', async () => {
    // Set current file
    diagramService.setCurrentFile({ id: 'existing-file-id', name: 'Existing.json' });
    fixture.detectChanges();
    
    // Get Save button and click it
    const saveButton = de.queryAll(By.css('.file-operations button'))[2];
    saveButton.nativeElement.click();
    
    // Wait for async operation
    await fixture.whenStable();
    
    // Verify saveDiagram was called without filename (save to existing file)
    expect(diagramService.saveDiagram).toHaveBeenCalledWith();
  });

  it('should show Save As dialog when Save As button is clicked', async () => {
    // Get Save As button and click it
    const saveAsButton = de.queryAll(By.css('.file-operations button'))[3];
    saveAsButton.nativeElement.click();
    
    // Wait for async operation
    await fixture.whenStable();
    
    // Verify showPicker was called with save mode
    expect(storageService.showPicker).toHaveBeenCalledWith(jasmine.objectContaining({
      mode: 'save'
    }));
    
    // Verify saveDiagram was called with filename
    expect(diagramService.saveDiagram).toHaveBeenCalledWith('Test Diagram.json');
  });

  it('should display file name and dirty indicator', () => {
    // Set current file and mark as dirty
    diagramService.setCurrentFile({ name: 'Current Diagram.json' });
    diagramService.setDirty(true);
    fixture.detectChanges();
    
    // Get file name display
    const fileNameDisplay = de.query(By.css('.file-name'));
    
    // Verify file name and dirty indicator
    expect(fileNameDisplay.nativeElement.textContent.trim()).toContain('Current Diagram.json*');
    expect(fileNameDisplay.classes['unsaved']).toBeTrue();
  });
});

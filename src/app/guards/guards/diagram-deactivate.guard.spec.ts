import { TestBed } from '@angular/core/testing';
import { TranslateService } from '@ngx-translate/core';
import { DiagramDeactivateGuard } from './diagram-deactivate.guard';
import { DiagramService } from '../../diagram/services/diagram.service';

describe('DiagramDeactivateGuard', () => {
  let guard: DiagramDeactivateGuard;
  let diagramServiceSpy: jasmine.SpyObj<DiagramService>;
  let translateServiceSpy: jasmine.SpyObj<TranslateService>;
  let confirmSpy: jasmine.Spy;

  beforeEach(() => {
    // Create spy for diagram service
    diagramServiceSpy = jasmine.createSpyObj('DiagramService', ['isDiagramDirty']);
    
    // Create spy for translate service
    translateServiceSpy = jasmine.createSpyObj('TranslateService', ['instant']);
    translateServiceSpy.instant.and.returnValue('Unsaved changes test message');
    
    // Create spy for window.confirm
    confirmSpy = spyOn(window, 'confirm');

    TestBed.configureTestingModule({
      providers: [
        DiagramDeactivateGuard,
        { provide: DiagramService, useValue: diagramServiceSpy },
        { provide: TranslateService, useValue: translateServiceSpy }
      ]
    });

    guard = TestBed.inject(DiagramDeactivateGuard);
  });

  it('should be created', () => {
    expect(guard).toBeTruthy();
  });
  
  it('should allow navigation when diagram has no unsaved changes', () => {
    // Configure diagram service to return false for isDiagramDirty
    diagramServiceSpy.isDiagramDirty.and.returnValue(false);
    
    // Check if canDeactivate returns true
    const result = guard.canDeactivate();
    expect(result).toBeTrue();
    
    // Confirm dialog should not be shown
    expect(confirmSpy).not.toHaveBeenCalled();
  });
  
  it('should show confirmation dialog when diagram has unsaved changes', () => {
    // Configure diagram service to return true for isDiagramDirty
    diagramServiceSpy.isDiagramDirty.and.returnValue(true);
    
    // Configure confirm to return true (user confirms leaving)
    confirmSpy.and.returnValue(true);
    
    // Check if canDeactivate returns true
    const result = guard.canDeactivate();
    expect(result).toBeTrue();
    
    // Confirm dialog should be shown with translated message
    expect(confirmSpy).toHaveBeenCalledWith('Unsaved changes test message');
    expect(translateServiceSpy.instant).toHaveBeenCalledWith('DIAGRAM.DEACTIVATE_GUARD.UNSAVED_CHANGES');
  });
  
  it('should prevent navigation when user cancels confirmation dialog', () => {
    // Configure diagram service to return true for isDiagramDirty
    diagramServiceSpy.isDiagramDirty.and.returnValue(true);
    
    // Configure confirm to return false (user cancels leaving)
    confirmSpy.and.returnValue(false);
    
    // Check if canDeactivate returns false
    const result = guard.canDeactivate();
    expect(result).toBeFalse();
    
    // Confirm dialog should be shown
    expect(confirmSpy).toHaveBeenCalled();
  });
});

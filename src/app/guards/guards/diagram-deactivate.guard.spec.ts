import { TestBed } from '@angular/core/testing';
import { DiagramDeactivateGuard } from './diagram-deactivate.guard';
import { DiagramService } from '../../diagram/services/diagram.service';

describe('DiagramDeactivateGuard', () => {
  let guard: DiagramDeactivateGuard;
  let diagramServiceSpy: jasmine.SpyObj<DiagramService>;

  beforeEach(() => {
    diagramServiceSpy = jasmine.createSpyObj('DiagramService', ['isDiagramDirty']);

    TestBed.configureTestingModule({
      providers: [
        DiagramDeactivateGuard,
        { provide: DiagramService, useValue: diagramServiceSpy }
      ]
    });

    guard = TestBed.inject(DiagramDeactivateGuard);
  });

  it('should be created', () => {
    expect(guard).toBeTruthy();
  });
});

import { TestBed } from '@angular/core/testing';
import { CanDeactivateFn } from '@angular/router';

import { diagramDeactivateGuard } from './diagram-deactivate.guard';

describe('diagramDeactivateGuard', () => {
  const executeGuard: CanDeactivateFn<unknown> = (...guardParameters) => 
      TestBed.runInInjectionContext(() => diagramDeactivateGuard(...guardParameters));

  beforeEach(() => {
    TestBed.configureTestingModule({});
  });

  it('should be created', () => {
    expect(executeGuard).toBeTruthy();
  });
});

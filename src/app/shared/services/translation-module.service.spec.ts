import { TestBed } from '@angular/core/testing';

import { TranslationModuleService } from './translation-module.service';

describe('TranslationModuleService', () => {
  let service: TranslationModuleService;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(TranslationModuleService);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
});

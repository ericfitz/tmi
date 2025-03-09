import { TestBed } from '@angular/core/testing';

import { GoogleStorageService } from './google-storage.service';

describe('GoogleStorageService', () => {
  let service: GoogleStorageService;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(GoogleStorageService);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
});

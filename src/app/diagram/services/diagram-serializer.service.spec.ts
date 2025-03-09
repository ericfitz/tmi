import { TestBed } from '@angular/core/testing';

import { DiagramSerializerService } from './diagram-serializer.service';

describe('DiagramSerializerService', () => {
  let service: DiagramSerializerService;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(DiagramSerializerService);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
});

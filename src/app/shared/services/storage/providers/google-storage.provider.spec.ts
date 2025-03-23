import { TestBed } from '@angular/core/testing';
import { GoogleStorageProvider } from './google-storage.provider';
import { LoggerService } from '../../logger/logger.service';

describe('GoogleStorageProvider', () => {
  let provider: GoogleStorageProvider;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;

  beforeEach(() => {
    const spy = jasmine.createSpyObj('LoggerService', ['debug', 'info', 'error']);
    
    TestBed.configureTestingModule({
      providers: [
        GoogleStorageProvider,
        { provide: LoggerService, useValue: spy }
      ]
    });
    
    provider = TestBed.inject(GoogleStorageProvider);
    loggerServiceSpy = TestBed.inject(LoggerService) as jasmine.SpyObj<LoggerService>;
  });

  it('should be created', () => {
    expect(provider).toBeTruthy();
  });
});

import { TestBed } from '@angular/core/testing';
import { StorageFactoryService } from './storage-factory.service';
import { LoggerService } from '../../logger/logger.service';
import { environment } from '../../../../../environments/environment';
import { GoogleStorageProvider } from './google-storage.provider';

describe('StorageFactoryService', () => {
  let service: StorageFactoryService;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;
  // Store original environment
  const originalEnv = { ...environment };

  beforeEach(() => {
    loggerServiceSpy = jasmine.createSpyObj('LoggerService', ['debug', 'warn']);

    TestBed.configureTestingModule({
      providers: [
        StorageFactoryService,
        { provide: LoggerService, useValue: loggerServiceSpy }
      ]
    });
    service = TestBed.inject(StorageFactoryService);
  });

  afterEach(() => {
    // Restore environment after each test
    (environment as any).storage = { ...originalEnv.storage };
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should create GoogleStorageProvider when provider is "google-drive"', () => {
    // Setup
    (environment as any).storage = { provider: 'google-drive' };
    
    // Execute
    const provider = service.createProvider();
    
    // Verify
    expect(provider instanceof GoogleStorageProvider).toBeTruthy();
    expect(loggerServiceSpy.debug).toHaveBeenCalledWith(
      'Creating storage provider: google-drive', 
      'StorageFactoryService'
    );
  });

  it('should create GoogleStorageProvider as default for unknown provider', () => {
    // Setup
    (environment as any).storage = { provider: 'unknown' };
    
    // Execute
    const provider = service.createProvider();
    
    // Verify
    expect(provider instanceof GoogleStorageProvider).toBeTruthy();
    expect(loggerServiceSpy.warn).toHaveBeenCalledWith(
      'Unknown provider type: unknown, using Google Drive', 
      'StorageFactoryService'
    );
  });
});
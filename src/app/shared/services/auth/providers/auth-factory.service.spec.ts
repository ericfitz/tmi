import { TestBed } from '@angular/core/testing';
import { AuthFactoryService } from './auth-factory.service';
import { GoogleAuthProvider } from './google-auth.provider';
import { AnonymousAuthProvider } from './anonymous-auth.provider';
import { LoggerService } from '../../logger/logger.service';
import { environment } from '../../../../../environments/environment';

describe('AuthFactoryService', () => {
  let service: AuthFactoryService;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;
  // Store original environment
  const originalEnv = { ...environment };

  beforeEach(() => {
    loggerServiceSpy = jasmine.createSpyObj('LoggerService', ['debug', 'warn']);

    TestBed.configureTestingModule({
      providers: [
        AuthFactoryService,
        { provide: LoggerService, useValue: loggerServiceSpy }
      ]
    });
    service = TestBed.inject(AuthFactoryService);
  });

  afterEach(() => {
    // Restore environment after each test
    (environment as any).auth = { ...originalEnv.auth };
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should create GoogleAuthProvider when provider is "google"', () => {
    // Setup
    (environment as any).auth = { provider: 'google' };
    
    // Execute
    const provider = service.createProvider();
    
    // Verify
    expect(provider instanceof GoogleAuthProvider).toBeTruthy();
    expect(loggerServiceSpy.debug).toHaveBeenCalledWith(
      'Creating auth provider: google', 
      'AuthFactoryService'
    );
  });

  it('should create AnonymousAuthProvider when provider is "anonymous"', () => {
    // Setup
    (environment as any).auth = { provider: 'anonymous' };
    
    // Execute
    const provider = service.createProvider();
    
    // Verify
    expect(provider instanceof AnonymousAuthProvider).toBeTruthy();
    expect(loggerServiceSpy.debug).toHaveBeenCalledWith(
      'Creating auth provider: anonymous', 
      'AuthFactoryService'
    );
  });

  it('should create AnonymousAuthProvider as default for unknown provider', () => {
    // Setup
    (environment as any).auth = { provider: 'unknown' };
    
    // Execute
    const provider = service.createProvider();
    
    // Verify
    expect(provider instanceof AnonymousAuthProvider).toBeTruthy();
    expect(loggerServiceSpy.warn).toHaveBeenCalledWith(
      'Unknown provider type: unknown, using anonymous', 
      'AuthFactoryService'
    );
  });
});
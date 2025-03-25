import { TestBed } from '@angular/core/testing';
import { CsrfService } from './csrf.service';
import { LoggerService } from '../logger/logger.service';

describe('CsrfService', () => {
  let service: CsrfService;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;

  beforeEach(() => {
    // Create spy for logger service
    const spy = jasmine.createSpyObj('LoggerService', ['debug']);

    TestBed.configureTestingModule({
      providers: [
        CsrfService,
        { provide: LoggerService, useValue: spy }
      ]
    });
    
    service = TestBed.inject(CsrfService);
    loggerServiceSpy = TestBed.inject(LoggerService) as jasmine.SpyObj<LoggerService>;
    
    // Clear session storage before each test
    sessionStorage.clear();
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should generate a token of correct length', () => {
    const token = service.generateToken();
    // 32 bytes = 64 hex characters
    expect(token.length).toBe(64);
    expect(loggerServiceSpy.debug).toHaveBeenCalledWith('CSRF token generated');
  });

  it('should retrieve the stored token', () => {
    const token = service.generateToken();
    const retrievedToken = service.getToken();
    expect(retrievedToken).toBe(token);
  });

  it('should return null when no token exists', () => {
    sessionStorage.clear();
    const retrievedToken = service.getToken();
    expect(retrievedToken).toBeNull();
  });

  it('should validate a valid token', () => {
    const token = service.generateToken();
    const isValid = service.validateToken(token);
    expect(isValid).toBeTrue();
  });

  it('should reject an invalid token', () => {
    service.generateToken();
    const isValid = service.validateToken('invalid-token');
    expect(isValid).toBeFalse();
  });

  it('should reject when no token exists', () => {
    sessionStorage.clear();
    const isValid = service.validateToken('some-token');
    expect(isValid).toBeFalse();
  });

  it('should provide the correct header key', () => {
    expect(service.getHeaderKey()).toBe('X-XSRF-TOKEN');
  });
});

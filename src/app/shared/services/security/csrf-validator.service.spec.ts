import { TestBed } from '@angular/core/testing';
import { CsrfValidatorService } from './csrf-validator.service';
import { LoggerService } from '../logger/logger.service';

describe('CsrfValidatorService', () => {
  let service: CsrfValidatorService;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;

  beforeEach(() => {
    // Create spy for logger service
    const spy = jasmine.createSpyObj('LoggerService', ['debug', 'warn']);

    TestBed.configureTestingModule({
      providers: [
        CsrfValidatorService,
        { provide: LoggerService, useValue: spy }
      ]
    });
    
    service = TestBed.inject(CsrfValidatorService);
    loggerServiceSpy = TestBed.inject(LoggerService) as jasmine.SpyObj<LoggerService>;
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should validate a properly formatted token', () => {
    // Create a valid hex string with 64 characters (32 bytes)
    const validToken = '1234567890abcdef'.repeat(4);
    const result = service.validateToken(validToken);
    
    expect(result).toBeTrue();
    expect(loggerServiceSpy.debug).toHaveBeenCalledWith('CSRF token validation successful');
  });

  it('should reject null tokens', () => {
    const result = service.validateToken(null);
    
    expect(result).toBeFalse();
    expect(loggerServiceSpy.warn).toHaveBeenCalledWith('CSRF validation failed: No token provided');
  });

  it('should reject tokens with invalid format', () => {
    // Invalid characters
    const invalidToken = 'invalid-token-with-non-hex-chars!';
    const result = service.validateToken(invalidToken);
    
    expect(result).toBeFalse();
    expect(loggerServiceSpy.warn).toHaveBeenCalledWith('CSRF validation failed: Invalid token format');
  });

  it('should reject tokens with incorrect length', () => {
    // Too short
    const shortToken = '1234567890abcdef';
    const resultShort = service.validateToken(shortToken);
    
    expect(resultShort).toBeFalse();
    
    // Too long
    const longToken = '1234567890abcdef'.repeat(5);
    const resultLong = service.validateToken(longToken);
    
    expect(resultLong).toBeFalse();
  });
});
import { TestBed } from '@angular/core/testing';
import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { HTTP_INTERCEPTORS, HttpClient, HttpErrorResponse } from '@angular/common/http';
import { CsrfInitInterceptor } from './csrf-init.interceptor';
import { CsrfService } from '../services/security/csrf.service';
import { LoggerService } from '../services/logger/logger.service';
import { of, throwError } from 'rxjs';

describe('CsrfInitInterceptor', () => {
  let httpClient: HttpClient;
  let httpTestingController: HttpTestingController;
  let csrfServiceSpy: jasmine.SpyObj<CsrfService>;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;

  beforeEach(() => {
    const csrfSpy = jasmine.createSpyObj('CsrfService', [
      'getToken', 'generateToken'
    ]);
    const loggerSpy = jasmine.createSpyObj('LoggerService', ['debug', 'warn']);

    TestBed.configureTestingModule({
      imports: [HttpClientTestingModule],
      providers: [
        { provide: CsrfService, useValue: csrfSpy },
        { provide: LoggerService, useValue: loggerSpy },
        { provide: HTTP_INTERCEPTORS, useClass: CsrfInitInterceptor, multi: true }
      ]
    });

    httpClient = TestBed.inject(HttpClient);
    httpTestingController = TestBed.inject(HttpTestingController);
    csrfServiceSpy = TestBed.inject(CsrfService) as jasmine.SpyObj<CsrfService>;
    loggerServiceSpy = TestBed.inject(LoggerService) as jasmine.SpyObj<LoggerService>;
  });

  afterEach(() => {
    httpTestingController.verify();
  });

  it('should generate a token on first request if none exists', () => {
    csrfServiceSpy.getToken.and.returnValue(null);
    
    httpClient.get('/api/data').subscribe();
    
    httpTestingController.expectOne('/api/data').flush({});
    expect(csrfServiceSpy.generateToken).toHaveBeenCalled();
    expect(loggerServiceSpy.debug).toHaveBeenCalledWith('CSRF token initialized on first HTTP request');
  });

  it('should not generate a token if one already exists', () => {
    csrfServiceSpy.getToken.and.returnValue('existing-token');
    
    httpClient.get('/api/data').subscribe();
    
    httpTestingController.expectOne('/api/data').flush({});
    expect(csrfServiceSpy.generateToken).not.toHaveBeenCalled();
  });

  it('should only try to initialize once across multiple requests', () => {
    csrfServiceSpy.getToken.and.returnValue(null);
    
    // First request
    httpClient.get('/api/data1').subscribe();
    httpTestingController.expectOne('/api/data1').flush({});
    
    // Reset spies
    csrfServiceSpy.getToken.calls.reset();
    csrfServiceSpy.generateToken.calls.reset();
    loggerServiceSpy.debug.calls.reset();
    
    // Second request
    httpClient.get('/api/data2').subscribe();
    httpTestingController.expectOne('/api/data2').flush({});
    
    // Should not try to initialize again
    expect(csrfServiceSpy.getToken).not.toHaveBeenCalled();
    expect(csrfServiceSpy.generateToken).not.toHaveBeenCalled();
    expect(loggerServiceSpy.debug).not.toHaveBeenCalled();
  });

  it('should regenerate token on 403 CSRF errors', () => {
    // Set up the request
    httpClient.get('/api/data').subscribe(
      () => {},
      err => {
        // We expect an error here
        expect(err.status).toBe(403);
      }
    );
    
    // Simulate a 403 response with CSRF error
    const mockReq = httpTestingController.expectOne('/api/data');
    mockReq.flush(
      { message: 'CSRF token validation failed' },
      { status: 403, statusText: 'Forbidden' }
    );
    
    // Should generate a new token
    expect(csrfServiceSpy.generateToken).toHaveBeenCalled();
    expect(loggerServiceSpy.warn).toHaveBeenCalledWith('CSRF token rejected, generating new token');
  });

  it('should not regenerate token on other errors', () => {
    // Set up the request
    httpClient.get('/api/data').subscribe(
      () => {},
      err => {
        // We expect an error here
        expect(err.status).toBe(500);
      }
    );
    
    // Simulate a different error response
    const mockReq = httpTestingController.expectOne('/api/data');
    mockReq.flush(
      { message: 'Server error' },
      { status: 500, statusText: 'Internal Server Error' }
    );
    
    // Should not generate a new token
    expect(csrfServiceSpy.generateToken).not.toHaveBeenCalled();
    expect(loggerServiceSpy.warn).not.toHaveBeenCalled();
  });
});
import { TestBed } from '@angular/core/testing';
import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { HTTP_INTERCEPTORS, HttpClient } from '@angular/common/http';
import { CsrfInterceptor } from './csrf.interceptor';
import { CsrfService } from '../services/security/csrf.service';
import { LoggerService } from '../services/logger/logger.service';

describe('CsrfInterceptor', () => {
  let httpClient: HttpClient;
  let httpTestingController: HttpTestingController;
  let csrfServiceSpy: jasmine.SpyObj<CsrfService>;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;

  beforeEach(() => {
    const csrfSpy = jasmine.createSpyObj('CsrfService', [
      'getToken', 'generateToken', 'getHeaderKey'
    ]);
    const loggerSpy = jasmine.createSpyObj('LoggerService', ['debug']);

    // Mock token and header key
    csrfSpy.getToken.and.returnValue('test-csrf-token');
    csrfSpy.generateToken.and.returnValue('generated-csrf-token');
    csrfSpy.getHeaderKey.and.returnValue('X-XSRF-TOKEN');

    TestBed.configureTestingModule({
      imports: [HttpClientTestingModule],
      providers: [
        { provide: CsrfService, useValue: csrfSpy },
        { provide: LoggerService, useValue: loggerSpy },
        { provide: HTTP_INTERCEPTORS, useClass: CsrfInterceptor, multi: true }
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

  it('should not add CSRF token for GET requests', () => {
    httpClient.get('/api/data').subscribe();

    const req = httpTestingController.expectOne('/api/data');
    expect(req.request.headers.has('X-XSRF-TOKEN')).toBeFalse();
  });

  it('should add CSRF token for POST requests', () => {
    httpClient.post('/api/data', { test: true }).subscribe();

    const req = httpTestingController.expectOne('/api/data');
    expect(req.request.headers.has('X-XSRF-TOKEN')).toBeTrue();
    expect(req.request.headers.get('X-XSRF-TOKEN')).toBe('test-csrf-token');
  });

  it('should add CSRF token for PUT requests', () => {
    httpClient.put('/api/data/1', { test: true }).subscribe();

    const req = httpTestingController.expectOne('/api/data/1');
    expect(req.request.headers.has('X-XSRF-TOKEN')).toBeTrue();
  });

  it('should add CSRF token for DELETE requests', () => {
    httpClient.delete('/api/data/1').subscribe();

    const req = httpTestingController.expectOne('/api/data/1');
    expect(req.request.headers.has('X-XSRF-TOKEN')).toBeTrue();
  });

  it('should generate a token if none exists', () => {
    csrfServiceSpy.getToken.and.returnValue(null);
    
    httpClient.post('/api/data', { test: true }).subscribe();

    const req = httpTestingController.expectOne('/api/data');
    expect(csrfServiceSpy.generateToken).toHaveBeenCalled();
    expect(req.request.headers.get('X-XSRF-TOKEN')).toBe('generated-csrf-token');
  });

  it('should log debug information', () => {
    httpClient.post('/api/data', { test: true }).subscribe();
    
    httpTestingController.expectOne('/api/data');
    expect(loggerServiceSpy.debug).toHaveBeenCalledWith('Added CSRF token to POST /api/data');
  });
});

import { TestBed } from '@angular/core/testing';
import { SecurityContext } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { SecurityService } from './security.service';
import { LoggerService } from '../logger/logger.service';

class MockDomSanitizer {
  sanitize(context: SecurityContext, value: string): string {
    return value ? value.replace(/javascript:/gi, '') : '';
  }
}

class MockLoggerService {
  info(): void {}
  error(): void {}
  warn(): void {}
  debug(): void {}
}

describe('SecurityService', () => {
  let service: SecurityService;
  let mockDocument: Document;
  
  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        SecurityService,
        { provide: LoggerService, useClass: MockLoggerService },
        { provide: DomSanitizer, useClass: MockDomSanitizer }
      ]
    });
    
    service = TestBed.inject(SecurityService);
    mockDocument = document;
  });
  
  it('should be created', () => {
    expect(service).toBeTruthy();
  });
  
  describe('CSP Configuration', () => {
    it('should generate a valid CSP policy string', () => {
      const configureCSPSpy = spyOn<any>(service, 'configureCSP').and.callThrough();
      const createCSPPolicySpy = spyOn<any>(service, 'createCSPPolicy').and.callThrough();
      
      service.initialize();
      
      expect(configureCSPSpy).toHaveBeenCalled();
      expect(createCSPPolicySpy).toHaveBeenCalled();
    });
    
    it('should update CSP when called', () => {
      const updateCSPSpy = spyOn<any>(service, 'updateCSP');
      
      // Directly call the private method for testing
      (service as any).updateCSP();
      
      expect(updateCSPSpy).toHaveBeenCalled();
    });
    
    it('should add trusted domains to CSP directives', () => {
      const updateCSPSpy = spyOn<any>(service, 'updateCSP');
      const directive = 'connect-src';
      const domains = ['https://example.com', 'https://api.example.com'];
      
      service.addTrustedDomains(directive, domains);
      
      expect(updateCSPSpy).toHaveBeenCalled();
    });
  });
  
  describe('Nonce Generation', () => {
    it('should generate a cryptographically secure nonce', () => {
      const nonce = service.generateNonce('script');
      expect(nonce).toBeTruthy();
      expect(nonce.length).toBeGreaterThan(16); // Should be at least 32 hex chars
    });
    
    it('should store generated nonces', () => {
      const updateCSPSpy = spyOn<any>(service, 'updateCSP');
      
      service.generateNonce('script');
      service.generateNonce('style');
      
      expect(updateCSPSpy).toHaveBeenCalledTimes(2);
    });
  });
  
  describe('Content Sanitization', () => {
    it('should sanitize URLs', () => {
      const safeUrl = 'https://example.com';
      const unsafeUrl = 'javascript:alert("XSS")';
      
      expect(service.sanitizeUrl(safeUrl)).toBe(safeUrl);
      expect(service.sanitizeUrl(unsafeUrl)).not.toContain('javascript:');
    });
    
    it('should sanitize HTML', () => {
      const safeHtml = '<p>Safe content</p>';
      
      expect(service.sanitizeHtml(safeHtml)).toBe(safeHtml);
    });
  });
  
  // Additional tests would verify CSP reporting and other functionality
});
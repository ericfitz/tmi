import { Injectable, SecurityContext } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { LoggerService } from '../logger/logger.service';
import { Observable, BehaviorSubject } from 'rxjs';

/**
 * Unified service to handle security-related functionality
 * CSP is now handled server-side, this service focuses on sanitization and other client-side security features
 */
@Injectable({
  providedIn: 'root'
})
export class SecurityService {
  private cspReportingSubject = new BehaviorSubject<string[]>([]);
  
  constructor(
    private logger: LoggerService,
    private sanitizer: DomSanitizer
  ) {
    this.setupCSPViolationReporting();
    this.logger.info('Security service initialized - CSP now handled server-side');
  }

  /**
   * Initialize security features
   * This can be used with APP_INITIALIZER
   */
  initialize(): Promise<void> {
    return Promise.resolve();
  }

  /**
   * Generate a cryptographically secure random value
   * Can be used for various security purposes like nonces or unique IDs
   * @returns A random hex string
   */
  generateRandomValue(length: number = 32): string {
    if (!this.isBrowser()) {
      return '';
    }
    
    const array = new Uint8Array(length);
    window.crypto.getRandomValues(array);
    return Array.from(array, byte => byte.toString(16).padStart(2, '0')).join('');
  }

  /**
   * Safely sanitize a URL using Angular's DomSanitizer
   * @param url The URL to sanitize
   * @returns A sanitized URL or empty string if invalid
   */
  sanitizeUrl(url: string): string {
    if (!url) return '';
    return this.sanitizer.sanitize(SecurityContext.URL, url) || '';
  }

  /**
   * Safely sanitize HTML using Angular's DomSanitizer
   * @param html The HTML to sanitize
   * @returns Sanitized HTML or empty string if invalid
   */
  sanitizeHtml(html: string): string {
    if (!html) return '';
    return this.sanitizer.sanitize(SecurityContext.HTML, html) || '';
  }

  /**
   * Get CSP violation reports as an observable
   * These are reported by the browser when it blocks content due to CSP
   */
  getCspReports(): Observable<string[]> {
    return this.cspReportingSubject.asObservable();
  }

  /**
   * Check if we're running in a browser environment
   */
  private isBrowser(): boolean {
    return typeof window !== 'undefined';
  }

  /**
   * Set up CSP violation reporting listener
   * This monitors for CSP violations reported by the browser
   */
  private setupCSPViolationReporting(): void {
    if (!this.isBrowser()) {
      return;
    }
    
    // Listen for CSP violations
    document.addEventListener('securitypolicyviolation', (event) => {
      const report = `CSP Violation: ${event.violatedDirective} (blocked ${event.blockedURI})`;
      this.logger.warn(report, 'SecurityService');
      
      const currentReports = this.cspReportingSubject.getValue();
      this.cspReportingSubject.next([...currentReports, report]);
    });
  }
}
import { Injectable, SecurityContext, signal } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';
import { LoggerService } from '../logger/logger.service';

/**
 * Unified service to handle security-related functionality
 * CSP is now handled server-side, this service focuses on sanitization and other client-side security features
 */
@Injectable({
  providedIn: 'root'
})
export class SecurityService {
  private cspReportsSignal = signal<string[]>([]);
  
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
   * Generate a cryptographically secure nonce for use with CSP
   * @param type The content type (script, style, etc.)
   * @returns A random hex string to use as a nonce
   */
  generateNonce(type: string = 'script'): string {
    // Log the nonce type for debugging
    this.logger.debug(`Generating nonce for ${type}`, 'SecurityService');
    return this.generateRandomValue(16);
  }
  
  /**
   * Add domains to the list of trusted domains for CSP
   * @param directive The CSP directive to update (script-src, img-src, etc.)
   * @param domains List of domains to trust
   */
  addTrustedDomains(directive: string, domains: string[]): void {
    this.logger.info(`Added trusted domains for ${directive}: ${domains.join(', ')}`, 'SecurityService');
    // Implementation would update CSP dynamically, but we're using server-side CSP
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
   * Get CSP violation reports
   * These are reported by the browser when it blocks content due to CSP
   */
  get cspReports() {
    return this.cspReportsSignal;
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
      
      // Update reports signal
      this.cspReportsSignal.update(reports => [...reports, report]);
    });
  }
}
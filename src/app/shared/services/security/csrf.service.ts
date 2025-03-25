import { Injectable } from '@angular/core';
import { LoggerService } from '../logger/logger.service';
import { HttpClient } from '@angular/common/http';

/**
 * Service for CSRF token management
 * Uses the double-submit cookie pattern where the server sets a cookie
 * and the client sends it back both as a cookie (automatic) and as a header
 */
@Injectable({
  providedIn: 'root'
})
export class CsrfService {
  private readonly tokenKey = 'XSRF-TOKEN';  // Cookie name
  private readonly headerKey = 'X-XSRF-TOKEN'; // Header name
  
  constructor(
    private logger: LoggerService,
    private http: HttpClient
  ) {}

  /**
   * Retrieves the CSRF token from cookies
   * @returns The stored token or null if not found
   */
  getToken(): string | null {
    return this.getCookie(this.tokenKey);
  }

  /**
   * Requests a new CSRF token from the server
   * The server will set the token as a cookie
   */
  refreshToken(): void {
    // This will trigger the server to set a new CSRF cookie
    this.http.get('/api/csrf-token', { observe: 'response' })
      .subscribe({
        next: () => this.logger.debug('CSRF token refreshed'),
        error: (err) => this.logger.error(`Error refreshing CSRF token: ${err.message}`)
      });
  }

  /**
   * Returns the header key used for CSRF protection
   */
  getHeaderKey(): string {
    return this.headerKey;
  }

  /**
   * Helper to get a cookie value by name
   * @param name The name of the cookie
   * @returns The cookie value or null if not found
   */
  private getCookie(name: string): string | null {
    if (typeof document === 'undefined') {
      return null; // Not in browser environment
    }
    
    const match = document.cookie.match(new RegExp('(^|;\\s*)(' + name + ')=([^;]*)'));
    return match ? decodeURIComponent(match[3]) : null;
  }

  /**
   * Validate a token format (for debugging/testing)
   * Actual validation happens on the server through the double-submit cookie pattern
   * @param token The token to validate
   * @returns True if valid format, false otherwise
   */
  validateTokenFormat(token: string): boolean {
    // Token should be a 64 character hex string (32 bytes)
    return /^[0-9a-f]{64}$/.test(token);
  }
}

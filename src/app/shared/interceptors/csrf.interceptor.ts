import { Injectable } from '@angular/core';
import { HttpInterceptor, HttpRequest, HttpHandler, HttpEvent } from '@angular/common/http';
import { Observable } from 'rxjs';
import { CsrfService } from '../services/security/csrf.service';
import { LoggerService } from '../services/logger/logger.service';

/**
 * Intercepts HTTP requests and adds the CSRF token from cookies to the X-XSRF-TOKEN header
 * for non-GET requests. Works with the server-side double-submit cookie pattern.
 */
@Injectable()
export class CsrfInterceptor implements HttpInterceptor {
  constructor(
    private csrfService: CsrfService,
    private logger: LoggerService
  ) {}

  intercept(request: HttpRequest<unknown>, next: HttpHandler): Observable<HttpEvent<unknown>> {
    // Skip CSRF protection for safe methods
    if (this.isSafeMutationMethod(request.method)) {
      return next.handle(request);
    }

    // Skip for cross-domain requests
    if (!this.isSameOrigin(request.url)) {
      return next.handle(request);
    }

    // Get token from cookie
    const token = this.csrfService.getToken();
    
    if (!token) {
      this.logger.warn(`No CSRF token available for ${request.method} ${request.url}`);
      // Try to fetch a new token if missing
      this.csrfService.refreshToken();
      return next.handle(request);
    }

    // Add CSRF token header to the request
    const headerKey = this.csrfService.getHeaderKey();
    const modifiedRequest = request.clone({
      headers: request.headers.set(headerKey, token)
    });

    this.logger.debug(`Added CSRF token to ${request.method} ${request.url}`);
    return next.handle(modifiedRequest);
  }

  /**
   * Check if the request method is safe from CSRF attacks
   * @param method The HTTP method
   * @returns True if the method is safe (GET, HEAD, OPTIONS), false otherwise
   */
  private isSafeMutationMethod(method: string): boolean {
    return ['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase());
  }
  
  /**
   * Check if the request URL is for the same origin as the current page
   * @param url The URL to check
   * @returns True if the URL is for the same origin
   */
  private isSameOrigin(url: string): boolean {
    // Handle relative URLs
    if (url.startsWith('/')) {
      return true;
    }
    
    try {
      const requestUrl = new URL(url);
      const originUrl = new URL(window.location.origin);
      return requestUrl.origin === originUrl.origin;
    } catch {
      // If parsing fails, assume it's not same origin
      return false;
    }
  }
}

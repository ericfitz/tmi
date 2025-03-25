import { Injectable } from '@angular/core';
import { HttpInterceptor, HttpRequest, HttpHandler, HttpEvent } from '@angular/common/http';
import { Observable } from 'rxjs';
import { tap } from 'rxjs/operators';
import { CsrfService } from '../services/security/csrf.service';
import { LoggerService } from '../services/logger/logger.service';

/**
 * Interceptor to ensure CSRF token is available
 * Will request a new token from the server when needed
 */
@Injectable()
export class CsrfInitInterceptor implements HttpInterceptor {
  private initialized = false;

  constructor(
    private csrfService: CsrfService,
    private logger: LoggerService
  ) {}

  intercept(request: HttpRequest<unknown>, next: HttpHandler): Observable<HttpEvent<unknown>> {
    // Skip token initialization for the token endpoint itself to avoid infinite loops
    if (request.url.includes('/api/csrf-token')) {
      return next.handle(request);
    }
    
    // Initialize CSRF token if not already done
    if (!this.initialized) {
      if (!this.csrfService.getToken()) {
        this.csrfService.refreshToken();
        this.logger.debug('CSRF token requested on first HTTP request');
      }
      this.initialized = true;
    }

    return next.handle(request).pipe(
      tap(
        // Success handler - no op
        (): void => { /* no operation needed */ }, 
        error => {
          // On HTTP error, check if we need to refresh the CSRF token
          if (error.status === 403 && (
              error.error?.error === 'CSRF token validation failed' || 
              error.error?.message?.includes('CSRF')
          )) {
            this.logger.warn('CSRF token rejected, requesting new token');
            this.csrfService.refreshToken();
          }
        }
      )
    );
  }
}
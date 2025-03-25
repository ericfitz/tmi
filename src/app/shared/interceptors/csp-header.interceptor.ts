import { Injectable } from '@angular/core';
import { HttpInterceptor, HttpRequest, HttpHandler, HttpEvent } from '@angular/common/http';
import { Observable } from 'rxjs';
import { LoggerService } from '../services/logger/logger.service';

/**
 * This interceptor is now deprecated as CSP headers are properly set by the server
 * Keeping this as a placeholder to avoid breaking changes in the app.config.ts
 */
@Injectable()
export class CspHeaderInterceptor implements HttpInterceptor {
  constructor(private logger: LoggerService) {
    this.logger.info('CSP Header Interceptor is deprecated - CSP is now handled server-side');
  }

  intercept(request: HttpRequest<unknown>, next: HttpHandler): Observable<HttpEvent<unknown>> {
    // Pass through without modification - CSP is set by the server
    return next.handle(request);
  }
}
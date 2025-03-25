import { Injectable } from '@angular/core';
import { LoggerService } from '../logger/logger.service';

/**
 * This service is now deprecated as CSRF validation is performed server-side
 * Keeping it for backwards compatibility, but all validation now happens on the server
 * using the double-submit cookie pattern.
 */
@Injectable({
  providedIn: 'root'
})
export class CsrfValidatorService {
  constructor(private logger: LoggerService) {
    this.logger.info('CsrfValidatorService is deprecated - validation now happens server-side');
  }

  /**
   * This method is now deprecated and only validates the format
   * Actual validation happens on the server through the double-submit cookie pattern
   * 
   * @param token The CSRF token to validate
   * @returns Always returns true as actual validation is server-side
   */
  validateToken(token: string | null): boolean {
    if (!token) {
      this.logger.debug('CSRF token format check: No token provided');
      return false;
    }

    // Only validate the format - real validation now happens server-side
    const isValidFormat = /^[0-9a-f]{64}$/.test(token);
    
    if (!isValidFormat) {
      this.logger.debug('CSRF token format check: Invalid token format');
      return false;
    }

    return true;
  }
}
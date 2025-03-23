import { Injectable } from '@angular/core';
import { environment } from '../../../../../environments/environment';
import { GoogleAuthProvider } from './google-auth.provider';
import { AnonymousAuthProvider } from './anonymous-auth.provider';
import { AuthProvider } from './auth-provider.interface';
import { LoggerService } from '../../logger/logger.service';

@Injectable({
  providedIn: 'root'
})
export class AuthFactoryService {
  constructor(private logger: LoggerService) {}

  /**
   * Create an authentication provider based on environment configuration
   */
  createProvider(): AuthProvider {
    const providerType = environment.auth.provider;
    this.logger.debug(`Creating auth provider: ${providerType}`, 'AuthFactoryService');
    
    switch (providerType) {
      case 'google':
        return new GoogleAuthProvider(this.logger);
      case 'anonymous':
        return new AnonymousAuthProvider(this.logger);
      default:
        this.logger.warn(`Unknown provider type: ${providerType}, using anonymous`, 'AuthFactoryService');
        return new AnonymousAuthProvider(this.logger);
    }
  }
}
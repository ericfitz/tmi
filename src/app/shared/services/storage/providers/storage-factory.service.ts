import { Injectable } from '@angular/core';
import { environment } from '../../../../../environments/environment';
import { GoogleStorageProvider } from './google-storage.provider';
import { StorageProvider } from './storage-provider.interface';
import { LoggerService } from '../../logger/logger.service';

@Injectable({
  providedIn: 'root'
})
export class StorageFactoryService {
  constructor(private logger: LoggerService) {}

  /**
   * Create a storage provider based on environment configuration
   */
  createProvider(): StorageProvider {
    const providerType = environment.storage.provider;
    this.logger.debug(`Creating storage provider: ${providerType}`, 'StorageFactoryService');
    
    switch (providerType) {
      case 'google-drive':
        return new GoogleStorageProvider(this.logger);
      default:
        this.logger.warn(`Unknown provider type: ${providerType}, using Google Drive`, 'StorageFactoryService');
        return new GoogleStorageProvider(this.logger);
    }
  }
}
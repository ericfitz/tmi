import { Injectable } from '@angular/core';
import { environment } from '../../../../../environments/environment';
import { GoogleStorageProvider } from './google-storage.provider';
import { StorageProvider, StorageFile, PickerOptions, PickerResult } from './storage-provider.interface';
import { LoggerService } from '../../logger/logger.service';
import { AuthService } from '../../auth/auth.service';

@Injectable({
  providedIn: 'root'
})
export class StorageFactoryService {
  constructor(
    private logger: LoggerService,
    private authService: AuthService
  ) {}

  /**
   * Create a storage provider based on environment configuration
   * and current authentication provider
   */
  createProvider(): StorageProvider {
    // If using anonymous auth, we should use local storage regardless of config
    const isAnonymous = this.authService.getUserInfo()?.id?.startsWith('anon_');
    
    let providerType = environment.storage.provider;
    
    // Override with local storage if using anonymous auth
    if (isAnonymous) {
      providerType = 'local-storage';
      this.logger.debug('Anonymous user detected, using local storage provider', 'StorageFactoryService');
    }
    
    this.logger.debug(`Creating storage provider: ${providerType}`, 'StorageFactoryService');
    
    switch (providerType) {
      case 'google-drive':
        return new GoogleStorageProvider(this.logger);
      case 'local-storage':
        return new LocalStorageProvider(this.logger);
      default:
        this.logger.warn(`Unknown provider type: ${providerType}, using local storage`, 'StorageFactoryService');
        return new LocalStorageProvider(this.logger);
    }
  }
}

/**
 * Local storage provider for development/testing
 * Stores files in browser's localStorage
 */
@Injectable()
export class LocalStorageProvider implements StorageProvider {
  private readonly prefix = 'tmi_files_';
  private readonly metadataKey = 'tmi_files_metadata';
  private initialized = false;
  private metadata: Record<string, StorageFile> = {};

  constructor(private logger: LoggerService) {
    this.loadMetadata();
  }

  private loadMetadata(): void {
    try {
      const stored = localStorage.getItem(this.metadataKey);
      if (stored) {
        this.metadata = JSON.parse(stored);
        
        // Convert date strings back to Date objects
        Object.values(this.metadata).forEach(file => {
          if (file.lastModified && typeof file.lastModified === 'string') {
            file.lastModified = new Date(file.lastModified);
          }
        });
      }
    } catch (e) {
      this.logger.error('Failed to load file metadata', 'LocalStorageProvider', e);
      this.metadata = {};
    }
  }

  private saveMetadata(): void {
    try {
      localStorage.setItem(this.metadataKey, JSON.stringify(this.metadata));
    } catch (e) {
      this.logger.error('Failed to save file metadata', 'LocalStorageProvider', e);
    }
  }

  private generateId(): string {
    return 'local_' + Math.random().toString(36).substring(2, 15);
  }

  async initialize(): Promise<boolean> {
    this.logger.info('Local storage provider initialized', 'LocalStorageProvider');
    this.initialized = true;
    return true;
  }

  isInitialized(): boolean {
    return this.initialized;
  }

  async createFile(name: string, data: string): Promise<StorageFile> {
    const id = this.generateId();
    const file: StorageFile = {
      id,
      name,
      mimeType: 'application/json',
      lastModified: new Date(),
      size: data.length,
      iconUrl: 'assets/icons/file.svg'
    };
    
    try {
      localStorage.setItem(this.prefix + id, data);
      this.metadata[id] = file;
      this.saveMetadata();
      
      this.logger.debug(`File created: ${name}`, 'LocalStorageProvider', { id });
      return file;
    } catch (e) {
      this.logger.error(`Failed to create file: ${name}`, 'LocalStorageProvider', e);
      throw new Error(`Failed to create file: ${e}`);
    }
  }

  async loadFile(fileId: string): Promise<string> {
    try {
      const data = localStorage.getItem(this.prefix + fileId);
      if (data === null) {
        throw new Error(`File not found: ${fileId}`);
      }
      
      this.logger.debug(`File loaded: ${fileId}`, 'LocalStorageProvider');
      return data;
    } catch (e) {
      this.logger.error(`Failed to load file: ${fileId}`, 'LocalStorageProvider', e);
      throw new Error(`Failed to load file: ${e}`);
    }
  }

  async saveFile(fileId: string, data: string): Promise<void> {
    try {
      if (!this.metadata[fileId]) {
        throw new Error(`File not found: ${fileId}`);
      }
      
      localStorage.setItem(this.prefix + fileId, data);
      
      // Update metadata
      this.metadata[fileId].lastModified = new Date();
      this.metadata[fileId].size = data.length;
      this.saveMetadata();
      
      this.logger.debug(`File saved: ${fileId}`, 'LocalStorageProvider');
    } catch (e) {
      this.logger.error(`Failed to save file: ${fileId}`, 'LocalStorageProvider', e);
      throw new Error(`Failed to save file: ${e}`);
    }
  }

  async listFiles(): Promise<StorageFile[]> {
    return Object.values(this.metadata).sort((a, b) => {
      if (a.lastModified && b.lastModified) {
        return b.lastModified.getTime() - a.lastModified.getTime();
      }
      return 0;
    });
  }

  async showPicker(options: PickerOptions): Promise<PickerResult> {
    if (options.mode === 'open') {
      // For 'open' mode, just show a simple list of files
      const files = await this.listFiles();
      if (files.length === 0) {
        this.logger.info('No files available to pick', 'LocalStorageProvider');
        return { action: 'canceled' };
      }
      
      // In a real implementation, you'd show a UI. For development, just pick the first file
      this.logger.info('Auto-picking first file for development', 'LocalStorageProvider');
      return {
        action: 'picked',
        file: files[0]
      };
    } else {
      // For 'save' mode, create a new file or pick an existing one
      this.logger.info('Auto-saving file for development', 'LocalStorageProvider');
      return {
        action: 'picked',
        fileName: options.initialFileName || 'New Diagram.json'
      };
    }
  }
}
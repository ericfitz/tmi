import { Injectable, Inject } from '@angular/core';
import { STORAGE_PROVIDER, StorageProvider, StorageFile, PickerOptions, PickerResult } from './providers/storage-provider.interface';
import { LoggerService } from '../logger/logger.service';
import { BehaviorSubject, Observable } from 'rxjs';

/**
 * Custom storage error class with improved type information
 */
export class StorageError extends Error {
  constructor(
    message: string,
    public readonly fileId?: string,
    public readonly fileName?: string,
    public readonly originalError?: unknown
  ) {
    super(message);
    this.name = 'StorageError';
  }
}

@Injectable({
  providedIn: 'root'
})
export class StorageService {
  private initialized = false;
  private initializing = false;
  private initializeSubject = new BehaviorSubject<boolean>(false);
  private currentFile = new BehaviorSubject<StorageFile | null>(null);

  constructor(
    @Inject(STORAGE_PROVIDER) private provider: StorageProvider,
    private logger: LoggerService
  ) {
    this.initialize();
  }

  /**
   * Initialize the storage provider
   */
  async initialize(): Promise<boolean> {
    if (this.initialized || this.initializing) {
      return this.initialized;
    }

    this.initializing = true;
    this.logger.debug('Initializing storage service', 'StorageService');

    try {
      this.initialized = await this.provider.initialize();
      this.logger.info(`Storage provider initialized: ${this.initialized}`, 'StorageService');
      this.initializeSubject.next(this.initialized);
      return this.initialized;
    } catch (error) {
      this.logger.error('Failed to initialize storage provider', 'StorageService', error);
      this.initialized = false;
      this.initializeSubject.next(false);
      return false;
    } finally {
      this.initializing = false;
    }
  }

  /**
   * Check if storage provider is initialized
   */
  isInitialized(): boolean {
    return this.initialized && this.provider.isInitialized();
  }

  /**
   * Get initialization state as observable
   */
  get initialized$(): Observable<boolean> {
    return this.initializeSubject.asObservable();
  }

  /**
   * Create a new file with the given name and data
   */
  async createFile(name: string, data: string): Promise<StorageFile> {
    this.logger.debug(`Creating file: ${name}`, 'StorageService');
    
    if (!this.isInitialized()) {
      await this.initialize();
    }
    
    try {
      const file = await this.provider.createFile(name, data);
      this.currentFile.next(file);
      this.logger.info(`File created: ${file.name}`, 'StorageService', { fileId: file.id });
      return file;
    } catch (error) {
      const storageError = new StorageError(
        `Failed to create file: ${name}`,
        undefined,
        name,
        error
      );
      this.logger.error(storageError.message, 'StorageService', error);
      throw storageError;
    }
  }
  
  /**
   * Get a file by ID
   */
  async getFile(fileId: string): Promise<StorageFile> {
    if (!this.isInitialized()) {
      await this.initialize();
    }
    
    const files = await this.listFiles();
    const file = files.find(f => f.id === fileId);
    
    if (!file) {
      throw new Error(`File with ID ${fileId} not found`);
    }
    
    return file;
  }
  
  /**
   * Update an existing file
   */
  async updateFile(fileId: string, data: string): Promise<StorageFile> {
    if (!this.isInitialized()) {
      await this.initialize();
    }
    
    await this.saveFile(fileId, data);
    return this.getFile(fileId);
  }

  /**
   * Load a file's content by ID
   */
  async loadFile(fileId: string): Promise<string> {
    this.logger.debug(`Loading file: ${fileId}`, 'StorageService');
    
    if (!this.isInitialized()) {
      await this.initialize();
    }
    
    try {
      const content = await this.provider.loadFile(fileId);
      this.logger.info(`File loaded: ${fileId}`, 'StorageService');
      
      // Update current file info if available from the list
      const files = await this.listFiles();
      const fileInfo = files.find(f => f.id === fileId);
      if (fileInfo) {
        this.currentFile.next(fileInfo);
      }
      
      return content;
    } catch (error) {
      this.logger.error(`Failed to load file: ${fileId}`, 'StorageService', error);
      throw error;
    }
  }

  /**
   * Save data to an existing file
   */
  async saveFile(fileId: string, data: string): Promise<void> {
    this.logger.debug(`Saving file: ${fileId}`, 'StorageService');
    
    if (!this.isInitialized()) {
      await this.initialize();
    }
    
    try {
      await this.provider.saveFile(fileId, data);
      this.logger.info(`File saved: ${fileId}`, 'StorageService');
    } catch (error) {
      const storageError = new StorageError(
        `Failed to save file: ${fileId}`,
        fileId,
        undefined,
        error
      );
      this.logger.error(storageError.message, 'StorageService', error);
      throw storageError;
    }
  }

  /**
   * List available files
   */
  async listFiles(): Promise<StorageFile[]> {
    this.logger.debug('Listing files', 'StorageService');
    
    if (!this.isInitialized()) {
      await this.initialize();
    }
    
    try {
      const files = await this.provider.listFiles();
      this.logger.info(`Found ${files.length} files`, 'StorageService');
      return files;
    } catch (error) {
      this.logger.error('Failed to list files', 'StorageService', error);
      throw error;
    }
  }

  /**
   * Show file picker for opening or saving files
   */
  async showPicker(options: PickerOptions): Promise<PickerResult> {
    this.logger.debug(`Showing picker in ${options.mode} mode`, 'StorageService');
    
    if (!this.isInitialized()) {
      await this.initialize();
    }
    
    try {
      const result = await this.provider.showPicker(options);
      
      if (result.action === 'picked' && result.file) {
        this.logger.info(`File picked: ${result.file.name}`, 'StorageService', { 
          fileId: result.file.id,
          mode: options.mode
        });
        
        // Update current file
        if (options.mode === 'open' && result.file) {
          this.currentFile.next(result.file);
        }
      } else {
        this.logger.debug('Picker canceled or no file selected', 'StorageService');
      }
      
      return result;
    } catch (error) {
      this.logger.error('Failed to show picker', 'StorageService', error);
      throw error;
    }
  }

  /**
   * Get the current active file
   */
  getCurrentFile(): StorageFile | null {
    return this.currentFile.getValue();
  }

  /**
   * Get the current file as observable
   */
  get currentFile$(): Observable<StorageFile | null> {
    return this.currentFile.asObservable();
  }
}
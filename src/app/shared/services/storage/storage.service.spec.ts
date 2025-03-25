import { TestBed } from '@angular/core/testing';

import { StorageService } from './storage.service';
import { LoggerService } from '../logger/logger.service';
import { StorageProvider, StorageFile, PickerOptions, PickerResult } from './providers/storage-provider.interface';

// Create a mock storage provider
class MockStorageProvider implements StorageProvider {
  private files: Map<string, any> = new Map();
  private _initialized = false;
  
  async initialize(): Promise<boolean> {
    this._initialized = true;
    return true;
  }

  isInitialized(): boolean {
    return this._initialized;
  }
  
  async createFile(name: string, data: string): Promise<StorageFile> {
    const id = `file-${Date.now()}`;
    const file: StorageFile = { 
      id, 
      name, 
      mimeType: 'application/json',
      size: data.length
    };
    // Store content separately since it's not part of StorageFile interface
    this.files.set(id, { ...file, content: data });
    return file;
  }

  async loadFile(fileId: string): Promise<string> {
    const file = this.files.get(fileId);
    if (!file) {
      throw new Error(`File with ID '${fileId}' not found`);
    }
    return file.content;
  }
  
  async saveFile(fileId: string, data: string): Promise<void> {
    const file = this.files.get(fileId);
    if (!file) {
      throw new Error(`File with ID '${fileId}' not found`);
    }
    
    this.files.set(fileId, { 
      ...file, 
      content: data,
      size: data.length
    });
  }
  
  async listFiles(): Promise<StorageFile[]> {
    const result: StorageFile[] = [];
    
    this.files.forEach(file => {
      // Exclude content from the list result
      const { content, ...fileInfo } = file;
      result.push(fileInfo as StorageFile);
    });
    
    return result;
  }
  
  async showPicker(options: PickerOptions): Promise<PickerResult> {
    // Mock a picked file
    if (options.mode === 'open') {
      // Find a random existing file
      const files = await this.listFiles();
      if (files.length > 0) {
        return {
          action: 'picked',
          file: files[0]
        };
      }
      
      // If no files, create a mock file
      const mockFile: StorageFile = {
        id: 'test-file-id',
        name: 'Test File.json',
        mimeType: 'application/json',
        size: 0
      };
      
      return {
        action: 'picked',
        file: mockFile
      };
    } else {
      return {
        action: 'picked',
        fileName: 'New File.json'
      };
    }
  }
}

// Mock Logger
class MockLoggerService {
  debug() {}
  info() {}
  warn() {}
  error() {}
}

describe('StorageService', () => {
  let service: StorageService;
  let storageProvider: MockStorageProvider;
  let logger: LoggerService;

  beforeEach(() => {
    storageProvider = new MockStorageProvider();
    
    TestBed.configureTestingModule({
      providers: [
        StorageService,
        { provide: LoggerService, useClass: MockLoggerService }
      ]
    });
    
    service = TestBed.inject(StorageService);
    logger = TestBed.inject(LoggerService);
    
    // Set the mock provider manually
    (service as any).provider = storageProvider;
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
  
  it('should create a new file with content', async () => {
    // Call createFile method
    const content = '{"data": "test data"}';
    const result = await service.createFile('new-file.json', content);
    
    // Check result
    expect(result).toBeTruthy();
    expect(result.name).toBe('new-file.json');
    expect(result.mimeType).toBe('application/json');
  });
  
  it('should list all files', async () => {
    // Create some test files
    await service.createFile('file1.json', '{}');
    await service.createFile('file2.json', '{}');
    
    // Call listFiles method
    const result = await service.listFiles();
    
    // Check result
    expect(result).toBeTruthy();
    expect(result.length).toBe(2);
    expect(result.map(f => f.name)).toContain('file1.json');
    expect(result.map(f => f.name)).toContain('file2.json');
  });
  
  it('should show file picker dialog', async () => {
    // Call showPicker method with open mode
    const openResult = await service.showPicker({ mode: 'open', title: 'Open File' });
    
    // Check open result
    expect(openResult.action).toBe('picked');
    expect(openResult.file).toBeTruthy();
    
    // Call showPicker method with save mode
    const saveResult = await service.showPicker({ mode: 'save', title: 'Save File' });
    
    // Check save result
    expect(saveResult.action).toBe('picked');
    expect(saveResult.fileName).toBe('New File.json');
  });
  
  it('should get file content by ID', async () => {
    // Create a test file
    const testContent = '{"test": true}';
    const file = await service.createFile('test.json', testContent);
    
    // Get content
    const content = await service.loadFile(file.id);
    
    // Check content
    expect(content).toBe(testContent);
  });
  
  it('should handle errors when file not found', async () => {
    try {
      // Try to get a non-existent file
      await service.loadFile('non-existent-id');
      fail('Expected error was not thrown');
    } catch (error) {
      expect(error).toBeTruthy();
    }
  });
});
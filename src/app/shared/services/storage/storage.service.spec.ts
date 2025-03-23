import { TestBed } from '@angular/core/testing';

import { StorageService } from './storage.service';
import { LoggerService } from '../logger/logger.service';
import { StorageProvider, StorageFile, PickerOptions, PickerResult } from './providers/storage-provider.interface';

// Create a mock storage provider
class MockStorageProvider implements StorageProvider {
  private files: Map<string, any> = new Map();
  
  getFile(id: string): Promise<StorageFile> {
    const file = this.files.get(id);
    if (!file) {
      return Promise.reject(new Error(`File with ID '${id}' not found`));
    }
    return Promise.resolve(file);
  }
  
  saveFile(name: string, content: string): Promise<StorageFile> {
    const id = `file-${Date.now()}`;
    const file = { id, name, content };
    this.files.set(id, file);
    return Promise.resolve(file);
  }
  
  updateFile(id: string, content: string): Promise<StorageFile> {
    const file = this.files.get(id);
    if (!file) {
      return Promise.reject(new Error(`File with ID '${id}' not found`));
    }
    
    const updatedFile = { ...file, content };
    this.files.set(id, updatedFile);
    return Promise.resolve(updatedFile);
  }
  
  deleteFile(id: string): Promise<void> {
    if (!this.files.has(id)) {
      return Promise.reject(new Error(`File with ID '${id}' not found`));
    }
    
    this.files.delete(id);
    return Promise.resolve();
  }
  
  listFiles(): Promise<StorageFile[]> {
    return Promise.resolve(Array.from(this.files.values()));
  }
  
  showPicker(options: PickerOptions): Promise<PickerResult> {
    // Mock a picked file
    if (options.mode === 'open') {
      return Promise.resolve({
        action: 'picked',
        file: { id: 'test-file-id', name: 'Test File.json' }
      });
    } else {
      return Promise.resolve({
        action: 'picked',
        fileName: 'New File.json'
      });
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
    (service as any).storageProvider = storageProvider;
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
  
  it('should get file content by ID', async () => {
    // Setup mock file
    const mockFile = { id: 'test-id', name: 'test.json', content: '{"test": true}' };
    (storageProvider as any).files.set('test-id', mockFile);
    
    // Call getFile method
    const result = await service.getFile('test-id');
    
    // Check result
    expect(result).toEqual(mockFile);
  });
  
  it('should save new file with content', async () => {
    // Call saveFile method
    const content = '{"data": "test data"}';
    const result = await service.saveFile('new-file.json', content);
    
    // Check result
    expect(result).toBeTruthy();
    expect(result.name).toBe('new-file.json');
    expect(result.content).toBe(content);
  });
  
  it('should update existing file', async () => {
    // Setup mock file
    const mockFile = { id: 'update-test-id', name: 'update-test.json', content: '{"test": true}' };
    (storageProvider as any).files.set('update-test-id', mockFile);
    
    // Call updateFile method
    const newContent = '{"test": false, "updated": true}';
    const result = await service.updateFile('update-test-id', newContent);
    
    // Check result
    expect(result).toBeTruthy();
    expect(result.id).toBe('update-test-id');
    expect(result.name).toBe('update-test.json');
    expect(result.content).toBe(newContent);
  });
  
  it('should list all files', async () => {
    // Setup mock files
    const files = [
      { id: 'file1', name: 'file1.json', content: '{}' },
      { id: 'file2', name: 'file2.json', content: '{}' }
    ];
    
    files.forEach(file => {
      (storageProvider as any).files.set(file.id, file);
    });
    
    // Call listFiles method
    const result = await service.listFiles();
    
    // Check result
    expect(result).toBeTruthy();
    expect(result.length).toBe(2);
    expect(result.map(f => f.id)).toContain('file1');
    expect(result.map(f => f.id)).toContain('file2');
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
  
  it('should handle errors when file not found', async () => {
    try {
      // Try to get a non-existent file
      await service.getFile('non-existent-id');
      fail('Expected error was not thrown');
    } catch (error) {
      expect(error).toBeTruthy();
    }
  });
});

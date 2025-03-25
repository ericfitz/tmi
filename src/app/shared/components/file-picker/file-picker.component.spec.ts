import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FilePickerComponent } from './file-picker.component';
import { StorageService } from '../../services/storage/storage.service';
import { LoggerService } from '../../services/logger/logger.service';
import { StorageFile } from '../../services/storage/providers/storage-provider.interface';
import { of } from 'rxjs';

describe('FilePickerComponent', () => {
  let component: FilePickerComponent;
  let fixture: ComponentFixture<FilePickerComponent>;
  let storageServiceSpy: jasmine.SpyObj<StorageService>;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;

  beforeEach(async () => {
    storageServiceSpy = jasmine.createSpyObj('StorageService', [
      'isInitialized',
      'initialize',
      'showPicker'
    ]);
    loggerServiceSpy = jasmine.createSpyObj('LoggerService', ['debug', 'info', 'error']);

    await TestBed.configureTestingModule({
      declarations: [FilePickerComponent],
      providers: [
        { provide: StorageService, useValue: storageServiceSpy },
        { provide: LoggerService, useValue: loggerServiceSpy }
      ]
    }).compileComponents();

    fixture = TestBed.createComponent(FilePickerComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('should initialize with default values', () => {
    expect(component.mode).toBe('open');
    expect(component.title).toBeUndefined();
    expect(component.buttonLabel).toBeUndefined();
    expect(component.acceptedFileTypes).toBeUndefined();
    expect(component.initialFileName).toBeUndefined();
  });

  it('should open picker in open mode', async () => {
    // Setup
    const mockFile: StorageFile = {
      id: 'file-1',
      name: 'test.json',
      mimeType: 'application/json',
      size: 100
    };
    
    storageServiceSpy.isInitialized.and.returnValue(true);
    storageServiceSpy.showPicker.and.returnValue(Promise.resolve({
      action: 'picked',
      file: mockFile
    }));
    
    // Setup spy for output
    spyOn(component.filePicked, 'emit');
    
    // Execute
    await component.openPicker();
    
    // Verify
    expect(storageServiceSpy.isInitialized).toHaveBeenCalled();
    expect(storageServiceSpy.initialize).not.toHaveBeenCalled();
    expect(storageServiceSpy.showPicker).toHaveBeenCalledWith({
      mode: 'open',
      title: undefined,
      buttonLabel: undefined,
      fileType: undefined,
      initialFileName: undefined
    });
    expect(component.filePicked.emit).toHaveBeenCalledWith(mockFile);
    expect(loggerServiceSpy.info).toHaveBeenCalled();
  });
  
  it('should initialize storage service if not already initialized', async () => {
    // Setup
    storageServiceSpy.isInitialized.and.returnValue(false);
    storageServiceSpy.initialize.and.returnValue(Promise.resolve(true));
    storageServiceSpy.showPicker.and.returnValue(Promise.resolve({
      action: 'canceled'
    }));
    
    spyOn(component.canceled, 'emit');
    
    // Execute
    await component.openPicker();
    
    // Verify
    expect(storageServiceSpy.isInitialized).toHaveBeenCalled();
    expect(storageServiceSpy.initialize).toHaveBeenCalled();
    expect(component.canceled.emit).toHaveBeenCalled();
  });
  
  it('should emit fileName in save mode', async () => {
    // Setup
    component.mode = 'save';
    
    storageServiceSpy.isInitialized.and.returnValue(true);
    storageServiceSpy.showPicker.and.returnValue(Promise.resolve({
      action: 'picked',
      fileName: 'new-file.json'
    }));
    
    spyOn(component.fileNameSelected, 'emit');
    
    // Execute
    await component.openPicker();
    
    // Verify
    expect(component.fileNameSelected.emit).toHaveBeenCalledWith('new-file.json');
  });
  
  it('should handle errors', async () => {
    // Setup
    const error = new Error('Test error');
    storageServiceSpy.isInitialized.and.throwError(error);
    
    // Execute
    await component.openPicker();
    
    // Verify
    expect(loggerServiceSpy.error).toHaveBeenCalledWith(
      'Error opening file picker', 
      'FilePickerComponent', 
      error
    );
  });
});
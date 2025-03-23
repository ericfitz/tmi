import { Component, EventEmitter, Input, Output } from '@angular/core';
import { StorageService } from '../../services/storage/storage.service';
import { StorageFile, PickerOptions } from '../../services/storage/providers/storage-provider.interface';
import { LoggerService } from '../../services/logger/logger.service';

@Component({
  selector: 'app-file-picker',
  templateUrl: './file-picker.component.html',
  styleUrls: ['./file-picker.component.scss']
})
export class FilePickerComponent {
  @Input() mode: 'open' | 'save' = 'open';
  @Input() title?: string;
  @Input() buttonLabel?: string;
  @Input() acceptedFileTypes?: string[];
  @Input() initialFileName?: string;
  
  @Output() filePicked = new EventEmitter<StorageFile>();
  @Output() fileNameSelected = new EventEmitter<string>();
  @Output() canceled = new EventEmitter<void>();
  
  constructor(
    private storageService: StorageService,
    private logger: LoggerService
  ) {}

  /**
   * Open the file picker
   */
  async openPicker(): Promise<void> {
    this.logger.debug(`Opening file picker in ${this.mode} mode`, 'FilePickerComponent');
    
    try {
      // Ensure storage service is initialized
      if (!this.storageService.isInitialized()) {
        await this.storageService.initialize();
      }
      
      // Prepare picker options
      const options: PickerOptions = {
        mode: this.mode,
        title: this.title,
        buttonLabel: this.buttonLabel,
        fileType: this.acceptedFileTypes,
        initialFileName: this.initialFileName
      };
      
      // Show the picker
      const result = await this.storageService.showPicker(options);
      
      // Handle the result
      if (result.action === 'picked') {
        this.logger.info('File selected from picker', 'FilePickerComponent');
        
        if (result.file) {
          this.filePicked.emit(result.file);
        }
        
        if (this.mode === 'save' && result.fileName) {
          this.fileNameSelected.emit(result.fileName);
        }
      } else {
        this.logger.debug('File picker canceled', 'FilePickerComponent');
        this.canceled.emit();
      }
    } catch (error) {
      this.logger.error('Error opening file picker', 'FilePickerComponent', error);
    }
  }
}
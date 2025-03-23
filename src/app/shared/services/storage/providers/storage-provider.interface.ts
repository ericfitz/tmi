export const STORAGE_PROVIDER = 'StorageProvider';

export interface StorageFile {
  id: string;
  name: string;
  mimeType: string;
  lastModified?: Date;
  size?: number;
  iconUrl?: string;
}

export interface PickerOptions {
  title?: string;
  buttonLabel?: string;
  mode: 'open' | 'save';
  fileType?: string[];
  initialFileName?: string;
}

export interface PickerResult {
  action: 'picked' | 'canceled';
  file?: StorageFile;
  fileName?: string;  // When saving a new file
}

export interface StorageProvider {
  initialize(): Promise<boolean>;
  isInitialized(): boolean;
  
  // File operations
  createFile(name: string, data: string): Promise<StorageFile>;
  loadFile(fileId: string): Promise<string>; // Returns file content
  saveFile(fileId: string, data: string): Promise<void>;
  listFiles(): Promise<StorageFile[]>;
  
  // Picker UI
  showPicker(options: PickerOptions): Promise<PickerResult>;
}
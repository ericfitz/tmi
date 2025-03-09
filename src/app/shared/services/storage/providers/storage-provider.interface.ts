export const STORAGE_PROVIDER = 'StorageProvider';

export interface StorageProvider {
  createFile(name: string, data: string): Promise<string>; // Returns file ID
  loadFile(fileId: string): Promise<string>; // Returns file content
  saveFile(fileId: string, data: string): Promise<void>;
  listFiles(): Promise<any[]>;
}
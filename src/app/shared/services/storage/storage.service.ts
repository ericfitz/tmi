import { Injectable, Inject } from '@angular/core';
import { STORAGE_PROVIDER, StorageProvider } from './providers/storage-provider.interface';

@Injectable({
  providedIn: 'root'
})
export class StorageService {
  constructor(@Inject(STORAGE_PROVIDER) private provider: StorageProvider) {}

  createFile(name: string, data: string): Promise<string> {
    return this.provider.createFile(name, data);
  }

  loadFile(fileId: string): Promise<string> {
    return this.provider.loadFile(fileId);
  }

  saveFile(fileId: string, data: string): Promise<void> {
    return this.provider.saveFile(fileId, data);
  }

  listFiles(): Promise<any[]> {
    return this.provider.listFiles();
  }
}
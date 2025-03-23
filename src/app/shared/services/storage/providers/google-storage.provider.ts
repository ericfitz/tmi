import { Injectable } from '@angular/core';
import { StorageProvider, StorageFile, PickerOptions, PickerResult } from './storage-provider.interface';
import { environment } from '../../../../../environments/environment';
import { LoggerService } from '../../logger/logger.service';

declare var gapi: any;
declare var google: any;

@Injectable()
export class GoogleStorageProvider implements StorageProvider {
  private initialized = false;
  private initializing = false;
  private tokenClient: any;
  private pickerApiLoaded = false;
  private driveApiLoaded = false;
  private oauthToken: string | null = null;
  
  // Configuration
  private clientId = environment.googleAuth.clientId;
  private apiKey = environment.storage.google.apiKey;
  private appId = environment.storage.google.appId;
  private mimeTypes = environment.storage.google.mimeTypes;
  private scope = 'https://www.googleapis.com/auth/drive.file';
  
  // Google Picker View IDs
  private readonly DOCS_VIEW = 'docs';
  private readonly DOCS_UPLOAD_VIEW = 'docsupload';

  constructor(private logger: LoggerService) {}

  /**
   * Initialize the Google Drive API and OAuth
   */
  async initialize(): Promise<boolean> {
    if (this.initialized) {
      return true;
    }

    if (this.initializing) {
      // Wait for initialization to complete
      return new Promise<boolean>((resolve) => {
        const checkInterval = setInterval(() => {
          if (!this.initializing) {
            clearInterval(checkInterval);
            resolve(this.initialized);
          }
        }, 100);
      });
    }

    this.initializing = true;
    this.logger.debug('Initializing Google Storage Provider', 'GoogleStorageProvider');

    try {
      // Load the required Google APIs
      await this.loadGoogleApis();
      
      // Initialize the token client for OAuth
      await this.setupTokenClient();
      
      this.initialized = true;
      this.logger.info('Google Storage Provider initialized', 'GoogleStorageProvider');
      return true;
    } catch (error) {
      this.logger.error('Failed to initialize Google Storage Provider', 'GoogleStorageProvider', error);
      return false;
    } finally {
      this.initializing = false;
    }
  }

  /**
   * Check if provider is initialized
   */
  isInitialized(): boolean {
    return this.initialized;
  }

  /**
   * Load the Google APIs (Drive, Picker)
   */
  private async loadGoogleApis(): Promise<void> {
    this.logger.debug('Loading Google APIs', 'GoogleStorageProvider');
    
    return new Promise<void>((resolve, reject) => {
      gapi.load('client:picker', async () => {
        try {
          // Initialize the gapi client with the API key
          await gapi.client.init({
            apiKey: this.apiKey,
            discoveryDocs: ['https://www.googleapis.com/discovery/v1/apis/drive/v3/rest']
          });
          
          // Load the Drive API
          await gapi.client.load('drive', 'v3');
          this.driveApiLoaded = true;
          this.logger.debug('Google Drive API loaded', 'GoogleStorageProvider');
          
          resolve();
        } catch (error) {
          this.logger.error('Failed to load Google APIs', 'GoogleStorageProvider', error);
          reject(error);
        }
      });
    });
  }

  /**
   * Set up the OAuth token client
   */
  private async setupTokenClient(): Promise<void> {
    this.logger.debug('Setting up token client', 'GoogleStorageProvider');
    
    this.tokenClient = google.accounts.oauth2.initTokenClient({
      client_id: this.clientId,
      scope: this.scope,
      callback: (tokenResponse: any) => {
        if (tokenResponse && tokenResponse.access_token) {
          this.oauthToken = tokenResponse.access_token;
          this.logger.debug('OAuth token received', 'GoogleStorageProvider');
        }
      }
    });
  }

  /**
   * Ensure user is authenticated with required scopes
   */
  private async ensureAuth(): Promise<boolean> {
    this.logger.debug('Ensuring authentication', 'GoogleStorageProvider');
    
    if (this.oauthToken) {
      return true;
    }
    
    return new Promise<boolean>((resolve) => {
      this.tokenClient.requestAccessToken({
        prompt: 'consent'
      });
      
      // Check for token at intervals
      const checkInterval = setInterval(() => {
        if (this.oauthToken) {
          clearInterval(checkInterval);
          resolve(true);
        }
      }, 100);
    });
  }

  /**
   * Create a new file in Google Drive
   */
  async createFile(name: string, data: string): Promise<StorageFile> {
    this.logger.debug(`Creating file: ${name}`, 'GoogleStorageProvider');
    
    if (!this.initialized) {
      await this.initialize();
    }
    
    await this.ensureAuth();
    
    // Prepare file metadata
    const metadata = {
      name: name,
      mimeType: 'application/json',
      parents: ['root'] // Save to the root folder by default
    };
    
    // Prepare the multipart request
    const boundary = '-------314159265358979323846';
    const delimiter = `\r\n--${boundary}\r\n`;
    const closeDelimiter = `\r\n--${boundary}--`;
    
    const multipartBody = 
      delimiter +
      'Content-Type: application/json\r\n\r\n' +
      JSON.stringify(metadata) +
      delimiter +
      'Content-Type: application/json\r\n\r\n' +
      data +
      closeDelimiter;
    
    try {
      // Create the file using Drive API
      const response = await gapi.client.request({
        path: 'https://www.googleapis.com/upload/drive/v3/files',
        method: 'POST',
        params: {
          uploadType: 'multipart',
          fields: 'id,name,mimeType,modifiedTime,size,iconLink'
        },
        headers: {
          'Content-Type': `multipart/related; boundary=${boundary}`,
          'Authorization': `Bearer ${this.oauthToken}`
        },
        body: multipartBody
      });
      
      const file = response.result;
      
      // Convert to StorageFile format
      const storageFile: StorageFile = {
        id: file.id,
        name: file.name,
        mimeType: file.mimeType,
        lastModified: file.modifiedTime ? new Date(file.modifiedTime) : undefined,
        size: file.size ? parseInt(file.size) : undefined,
        iconUrl: file.iconLink
      };
      
      this.logger.info(`File created: ${storageFile.name}`, 'GoogleStorageProvider', { fileId: storageFile.id });
      return storageFile;
    } catch (error) {
      this.logger.error(`Failed to create file: ${name}`, 'GoogleStorageProvider', error);
      throw error;
    }
  }

  /**
   * Load a file from Google Drive
   */
  async loadFile(fileId: string): Promise<string> {
    this.logger.debug(`Loading file: ${fileId}`, 'GoogleStorageProvider');
    
    if (!this.initialized) {
      await this.initialize();
    }
    
    await this.ensureAuth();
    
    try {
      // Fetch the file content
      const response = await gapi.client.drive.files.get({
        fileId: fileId,
        alt: 'media'
      });
      
      this.logger.info(`File loaded: ${fileId}`, 'GoogleStorageProvider');
      return response.body;
    } catch (error) {
      this.logger.error(`Failed to load file: ${fileId}`, 'GoogleStorageProvider', error);
      throw error;
    }
  }

  /**
   * Save data to an existing file
   */
  async saveFile(fileId: string, data: string): Promise<void> {
    this.logger.debug(`Saving file: ${fileId}`, 'GoogleStorageProvider');
    
    if (!this.initialized) {
      await this.initialize();
    }
    
    await this.ensureAuth();
    
    try {
      // Update the file content using Drive API
      await gapi.client.request({
        path: `https://www.googleapis.com/upload/drive/v3/files/${fileId}`,
        method: 'PATCH',
        params: {
          uploadType: 'media'
        },
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${this.oauthToken}`
        },
        body: data
      });
      
      this.logger.info(`File saved: ${fileId}`, 'GoogleStorageProvider');
    } catch (error) {
      this.logger.error(`Failed to save file: ${fileId}`, 'GoogleStorageProvider', error);
      throw error;
    }
  }

  /**
   * List files from Google Drive
   */
  async listFiles(): Promise<StorageFile[]> {
    this.logger.debug('Listing files', 'GoogleStorageProvider');
    
    if (!this.initialized) {
      await this.initialize();
    }
    
    await this.ensureAuth();
    
    try {
      // Build the query to filter by mime types
      let query = '';
      if (this.mimeTypes && this.mimeTypes.length > 0) {
        query = this.mimeTypes.map(mimeType => `mimeType='${mimeType}'`).join(' or ');
      }
      
      // Fetch the file list
      const response = await gapi.client.drive.files.list({
        q: query,
        pageSize: 100,
        fields: 'files(id, name, mimeType, modifiedTime, size, iconLink)'
      });
      
      // Convert to StorageFile format
      const files: StorageFile[] = response.result.files.map((file: any) => ({
        id: file.id,
        name: file.name,
        mimeType: file.mimeType,
        lastModified: file.modifiedTime ? new Date(file.modifiedTime) : undefined,
        size: file.size ? parseInt(file.size) : undefined,
        iconUrl: file.iconLink
      }));
      
      this.logger.info(`Found ${files.length} files`, 'GoogleStorageProvider');
      return files;
    } catch (error) {
      this.logger.error('Failed to list files', 'GoogleStorageProvider', error);
      throw error;
    }
  }

  /**
   * Show the Google Picker UI
   */
  async showPicker(options: PickerOptions): Promise<PickerResult> {
    this.logger.debug(`Showing picker: ${options.mode}`, 'GoogleStorageProvider');
    
    if (!this.initialized) {
      await this.initialize();
    }
    
    await this.ensureAuth();
    
    // Load the picker API if not loaded yet
    if (!this.pickerApiLoaded) {
      await this.loadPickerApi();
    }
    
    return new Promise<PickerResult>((resolve) => {
      // Configure picker views based on mode
      const views = [];
      
      // For open mode, show existing documents
      if (options.mode === 'open') {
        let view = new google.picker.View(google.picker.ViewId[this.DOCS_VIEW]);
        
        // Apply file type filter if specified
        if (options.fileType && options.fileType.length > 0) {
          view.setMimeTypes(options.fileType.join(','));
        } else if (this.mimeTypes && this.mimeTypes.length > 0) {
          view.setMimeTypes(this.mimeTypes.join(','));
        }
        
        views.push(view);
      }
      
      // For save mode or as a fallback, show upload option
      if (options.mode === 'save' || views.length === 0) {
        let uploadView = new google.picker.DocsUploadView();
        
        if (options.fileType && options.fileType.length > 0) {
          uploadView.setMimeTypes(options.fileType.join(','));
        } else if (this.mimeTypes && this.mimeTypes.length > 0) {
          uploadView.setMimeTypes(this.mimeTypes.join(','));
        }
        
        views.push(uploadView);
      }
      
      // Create and render the picker
      const picker = new google.picker.PickerBuilder()
        .addView(google.picker.ViewId.DOCS)
        .setOAuthToken(this.oauthToken || '')
        .setDeveloperKey(this.apiKey)
        .setAppId(this.appId)
        .setTitle(options.title || (options.mode === 'open' ? 'Open File' : 'Save File'))
        .setSelectableMimeTypes(this.mimeTypes.join(','))
        .setCallback(async (data: any) => {
          // Handle picker events
          if (data.action === google.picker.Action.PICKED) {
            const doc = data.docs[0];
            
            const file: StorageFile = {
              id: doc.id,
              name: doc.name,
              mimeType: doc.mimeType,
              lastModified: doc.lastEditedUtc ? new Date(doc.lastEditedUtc) : undefined,
              size: doc.sizeBytes ? parseInt(doc.sizeBytes) : undefined,
              iconUrl: doc.iconUrl
            };
            
            this.logger.info(`File picked: ${file.name}`, 'GoogleStorageProvider', { fileId: file.id });
            
            resolve({
              action: 'picked',
              file
            });
          } else if (data.action === google.picker.Action.CANCEL) {
            this.logger.debug('Picker canceled', 'GoogleStorageProvider');
            resolve({
              action: 'canceled'
            });
          }
        });
      
      // Add all views to the picker
      views.forEach(view => picker.addView(view));
      
      // For save mode, show filename input
      if (options.mode === 'save') {
        picker.setCallback((data: any) => {
          if (data.action === google.picker.Action.PICKED) {
            const doc = data.docs[0];
            
            const file: StorageFile = {
              id: doc.id,
              name: doc.name,
              mimeType: doc.mimeType,
              lastModified: doc.lastEditedUtc ? new Date(doc.lastEditedUtc) : undefined,
              size: doc.sizeBytes ? parseInt(doc.sizeBytes) : undefined,
              iconUrl: doc.iconUrl
            };
            
            this.logger.info(`File picked for save: ${file.name}`, 'GoogleStorageProvider', { fileId: file.id });
            
            resolve({
              action: 'picked',
              file,
              fileName: doc.name
            });
          } else if (data.action === google.picker.Action.CANCEL) {
            this.logger.debug('Picker canceled', 'GoogleStorageProvider');
            resolve({
              action: 'canceled'
            });
          }
        });
        
        // If initial filename provided, set it
        if (options.initialFileName) {
          picker.setTitle(`Save as: ${options.initialFileName}`);
        }
      }
      
      // Render the picker
      picker.build().setVisible(true);
    });
  }

  /**
   * Load the Google Picker API
   */
  private async loadPickerApi(): Promise<void> {
    this.logger.debug('Loading Google Picker API', 'GoogleStorageProvider');
    
    return new Promise<void>((resolve, reject) => {
      gapi.load('picker', {
        callback: () => {
          this.pickerApiLoaded = true;
          this.logger.debug('Google Picker API loaded', 'GoogleStorageProvider');
          resolve();
        },
        onerror: (error: any) => {
          this.logger.error('Failed to load Google Picker API', 'GoogleStorageProvider', error);
          reject(error);
        }
      });
    });
  }
}
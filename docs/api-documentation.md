# TMI API Documentation

This document provides comprehensive API documentation for the core services and components of the TMI application.

## Table of Contents

1. [Diagram Services](#diagram-services)
   - [DiagramService](#diagramservice)
   - [DiagramRendererService](#diagramrendererservice)
   - [DiagramStateService](#diagramstateservice)
   - [DiagramSerializerService](#diagramserializerservice)

2. [Authentication Services](#authentication-services)
   - [AuthService](#authservice)
   - [GoogleAuthProvider](#googleauthprovider)
   - [AnonymousAuthProvider](#anonymousauthprovider)
   - [AuthFactoryService](#authfactoryservice)

3. [Storage Services](#storage-services)
   - [StorageService](#storageservice)
   - [GoogleStorageProvider](#googlestorageprovider)
   - [StorageFactoryService](#storagefactoryservice)

4. [Translation Services](#translation-services)
   - [TranslationService](#translationservice)

5. [Security Services](#security-services)
   - [SecurityService](#securityservice)
   - [CsrfService](#csrfservice)
   - [CsrfValidatorService](#csrfvalidatorservice)

6. [Utility Services](#utility-services)
   - [LoggerService](#loggerservice)

---

## Diagram Services

### DiagramService

Core service for managing diagram operations including initialization, editing, and persistence.

**Location:** `/src/app/diagram/services/diagram.service.ts`

#### Properties

| Name | Type | Description |
|------|------|-------------|
| `currentDiagram$` | `Observable<DiagramData \| null>` | Observable of the current diagram data |
| `isDirty$` | `Observable<boolean>` | Observable indicating if diagram has unsaved changes |
| `currentFile$` | `Observable<StorageFile \| null>` | Observable of the current storage file |

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `initGraph` | `container: HTMLElement` | `MxGraph` | Initializes a new diagram graph in the provided container |
| `addNode` | `x: number, y: number, width: number, height: number, label: string, style?: string` | `MxGraphCell` | Adds a node to the graph |
| `addEdge` | `source: MxGraphCell, target: MxGraphCell, label: string, style?: string` | `MxGraphCell` | Adds an edge between two nodes |
| `deleteSelected` | none | `void` | Deletes selected elements from the diagram |
| `exportDiagram` | none | `DiagramData` | Exports the current diagram as data model |
| `importDiagram` | `diagramData: DiagramData` | `void` | Imports a diagram from data model |
| `saveDiagram` | `fileName?: string` | `Promise<StorageFile>` | Saves the current diagram to storage |
| `loadDiagram` | `fileId: string` | `Promise<void>` | Loads a diagram from storage by ID |
| `loadDiagramList` | none | `Promise<StorageFile[]>` | Loads list of diagrams from storage |
| `getCurrentDiagram` | none | `DiagramData \| null` | Gets the current diagram data |
| `getCurrentFile` | none | `StorageFile \| null` | Gets the current storage file |
| `isDiagramDirty` | none | `boolean` | Checks if diagram has unsaved changes |
| `resetDiagram` | none | `void` | Resets the diagram to a blank state |

#### Usage Example

```typescript
// Initialize a diagram
const graph = this.diagramService.initGraph(containerElement);

// Add elements
const node1 = this.diagramService.addNode(100, 100, 120, 60, 'Node 1');
const node2 = this.diagramService.addNode(300, 100, 120, 60, 'Node 2');
this.diagramService.addEdge(node1, node2, 'Connection');

// Save the diagram
const file = await this.diagramService.saveDiagram('My Diagram');
```

### DiagramRendererService

Service responsible for rendering diagram elements to the canvas.

**Location:** `/src/app/diagram/services/diagram-renderer.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `updateGraph` | `graph: DiagramGraph, elements: DiagramElement[], settings: { zoomLevel: number, showGrid: boolean, gridSize: number, backgroundColor: string, selectedElementIds: string[] }` | `void` | Updates the graph with elements and settings |
| `updateSelectedElements` | `graph: DiagramGraph, selectedIds: string[]` | `void` | Updates selected elements in the graph |
| `batchUpdateElements` | `graph: DiagramGraph, elements: DiagramElement[]` | `void` | Performs batched updates to diagram elements |
| `addElementToGraph` | `graph: DiagramGraph, element: DiagramElement` | `void` | Adds a new element to the graph |
| `updateElementInGraph` | `graph: DiagramGraph, cell: DiagramCell, element: DiagramElement` | `void` | Updates an existing element in the graph |
| `getStyleForElement` | `element: DiagramElement` | `string` | Gets the style string for an element |

#### Usage Example

```typescript
// Update the diagram with elements and settings
this.diagramRenderer.updateGraph(this.graph, elements, {
  zoomLevel: 1.0,
  showGrid: true,
  gridSize: 20,
  backgroundColor: '#ffffff',
  selectedElementIds: ['element-1', 'element-2']
});
```

### DiagramStateService

Service for managing diagram state using the NgRx store.

**Location:** `/src/app/diagram/services/diagram-state.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `loadDiagram` | `diagramId: string` | `void` | Dispatches action to load diagram by ID |
| `saveDiagram` | none | `void` | Dispatches action to save current diagram |
| `createDiagram` | `name: string, properties?: Record<string, unknown>` | `void` | Dispatches action to create new diagram |
| `addElement` | `element: Partial<DiagramElement>` | `void` | Dispatches action to add element |
| `updateElement` | `id: string, changes: Partial<DiagramElement>` | `void` | Dispatches action to update element |
| `removeElement` | `id: string` | `void` | Dispatches action to remove element |
| `selectElement` | `id: string \| null` | `void` | Dispatches action to select element |
| `undo` | none | `void` | Dispatches action to undo last change |
| `redo` | none | `void` | Dispatches action to redo last undone change |
| `toggleGrid` | `show: boolean` | `void` | Dispatches action to toggle grid visibility |

#### Usage Example

```typescript
// Create a new diagram
this.diagramStateService.createDiagram('New Diagram', {
  backgroundColor: '#f8f8f8',
  gridSize: 10,
  snapToGrid: true
});

// Add an element
this.diagramStateService.addElement({
  type: DiagramElementType.RECTANGLE,
  position: { x: 100, y: 100 },
  size: { width: 120, height: 60 },
  properties: { text: 'New Element' }
});
```

### DiagramSerializerService

Service for serializing and deserializing diagram data to/from various formats.

**Location:** `/src/app/diagram/services/diagram-serializer.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `serializeToPng` | `diagram: DiagramData, scale?: number` | `Promise<string>` | Serializes diagram to PNG image data URL |
| `serializeToJson` | `diagram: DiagramData` | `string` | Serializes diagram to JSON string |
| `deserializeFromJson` | `json: string` | `DiagramData` | Deserializes diagram from JSON string |

#### Usage Example

```typescript
// Export diagram as JSON
const jsonData = this.diagramSerializerService.serializeToJson(diagramData);

// Export diagram as PNG
const pngDataUrl = await this.diagramSerializerService.serializeToPng(diagramData, 2.0);
```

---

## Authentication Services

### AuthService

Main service for authentication and user management.

**Location:** `/src/app/shared/services/auth/auth.service.ts`

#### Properties

| Name | Type | Description |
|------|------|-------------|
| `currentUser$` | `Observable<UserInfo \| null>` | Observable of the current user information |
| `isAuthenticated$` | `Observable<boolean>` | Observable indicating if user is authenticated |
| `authProvider$` | `Observable<AuthProvider>` | Observable of the current auth provider |

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `login` | none | `Promise<void>` | Initiates the login process |
| `logout` | none | `Promise<void>` | Logs out the current user |
| `silentSignIn` | none | `Promise<boolean>` | Attempts to sign in silently using stored credentials |
| `getAuthProvider` | none | `AuthProvider` | Gets the current auth provider |
| `getUserInfo` | none | `UserInfo \| null` | Gets the current user information |
| `isAuthenticated` | none | `boolean` | Checks if user is authenticated |

#### Usage Example

```typescript
// Log in
await this.authService.login();

// Get user info
const userInfo = this.authService.getUserInfo();
if (userInfo) {
  console.log(`Logged in as: ${userInfo.name} (${userInfo.email})`);
}

// Log out
await this.authService.logout();
```

### GoogleAuthProvider

Provider for Google authentication.

**Location:** `/src/app/shared/services/auth/providers/google-auth.provider.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `login` | none | `Promise<void>` | Initiates Google login process |
| `logout` | none | `Promise<void>` | Logs out from Google |
| `silentSignIn` | none | `Promise<boolean>` | Attempts to sign in silently using stored credentials |
| `isAuthenticated` | none | `boolean` | Checks if user is authenticated with Google |
| `getUserInfo` | none | `UserInfo \| null` | Gets the authenticated user information |

#### Usage Example

```typescript
// Login with Google
await this.googleAuthProvider.login();

// Check if authenticated
if (this.googleAuthProvider.isAuthenticated()) {
  const userInfo = this.googleAuthProvider.getUserInfo();
  console.log(`Authenticated as: ${userInfo?.email}`);
}
```

### AnonymousAuthProvider

Provider for anonymous authentication.

**Location:** `/src/app/shared/services/auth/providers/anonymous-auth.provider.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `login` | none | `Promise<void>` | Creates an anonymous session |
| `logout` | none | `Promise<void>` | Ends the anonymous session |
| `isAuthenticated` | none | `boolean` | Always returns true for anonymous auth |
| `getUserInfo` | none | `UserInfo` | Returns generic anonymous user info |
| `silentSignIn` | none | `Promise<boolean>` | Always succeeds for anonymous auth |

#### Usage Example

```typescript
// Login anonymously
await this.anonymousAuthProvider.login();

// Get anonymous user info
const anonUser = this.anonymousAuthProvider.getUserInfo();
console.log(`Anonymous user ID: ${anonUser.id}`);
```

### AuthFactoryService

Factory service for creating authentication providers.

**Location:** `/src/app/shared/services/auth/providers/auth-factory.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `createProvider` | none | `AuthProvider` | Creates appropriate auth provider based on configuration |

#### Usage Example

```typescript
// Create auth provider
const authProvider = this.authFactoryService.createProvider();

// Use the provider
await authProvider.login();
```

---

## Storage Services

### StorageService

Main service for file storage and retrieval.

**Location:** `/src/app/shared/services/storage/storage.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `createFile` | `name: string, data: string` | `Promise<StorageFile>` | Creates a new file in storage |
| `loadFile` | `fileId: string` | `Promise<string>` | Loads file content by ID |
| `saveFile` | `fileId: string, data: string` | `Promise<void>` | Saves data to an existing file |
| `listFiles` | none | `Promise<StorageFile[]>` | Lists available files |
| `showPicker` | `options: PickerOptions` | `Promise<PickerResult>` | Shows file picker UI |
| `getStorageProvider` | none | `StorageProvider` | Gets the current storage provider |

#### Usage Example

```typescript
// List files
const files = await this.storageService.listFiles();
console.log(`Found ${files.length} files`);

// Create a new file
const file = await this.storageService.createFile('My Document.json', '{"title":"Hello World"}');

// Load a file
const content = await this.storageService.loadFile(file.id);

// Save changes to a file
await this.storageService.saveFile(file.id, '{"title":"Updated Content"}');
```

### GoogleStorageProvider

Storage provider using Google Drive.

**Location:** `/src/app/shared/services/storage/providers/google-storage.provider.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `initialize` | none | `Promise<boolean>` | Initializes the Google Drive API |
| `isInitialized` | none | `boolean` | Checks if provider is initialized |
| `createFile` | `name: string, data: string` | `Promise<StorageFile>` | Creates a new file in Google Drive |
| `loadFile` | `fileId: string` | `Promise<string>` | Loads file content from Google Drive |
| `saveFile` | `fileId: string, data: string` | `Promise<void>` | Saves data to a Google Drive file |
| `listFiles` | none | `Promise<StorageFile[]>` | Lists files from Google Drive |
| `showPicker` | `options: PickerOptions` | `Promise<PickerResult>` | Shows Google Drive picker UI |

#### Usage Example

```typescript
// Initialize Google Storage
await this.googleStorageProvider.initialize();

// Show picker to open a file
const result = await this.googleStorageProvider.showPicker({ 
  mode: 'open',
  title: 'Open Diagram' 
});

// Load the selected file
if (result.action === 'picked' && result.file) {
  const content = await this.googleStorageProvider.loadFile(result.file.id);
  console.log(`Loaded file: ${result.file.name}`);
}
```

### StorageFactoryService

Factory service for creating storage providers.

**Location:** `/src/app/shared/services/storage/providers/storage-factory.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `createProvider` | none | `StorageProvider` | Creates appropriate storage provider based on configuration |

#### Usage Example

```typescript
// Create storage provider
const storageProvider = this.storageFactoryService.createProvider();

// Use the provider
await storageProvider.initialize();
```

---

## Translation Services

### TranslationService

Service for internationalization and language management.

**Location:** `/src/app/shared/services/i18n/translation.service.ts`

#### Properties

| Name | Type | Description |
|------|------|-------------|
| `availableLanguages` | `Record<SupportedLanguages, LanguageInfo>` | Available languages with their info |

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `initialize` | none | `Promise<void>` | Initializes translations |
| `changeLanguage` | `lang: string` | `Observable<SupportedLanguages>` | Changes the current language |
| `getCurrentLanguage` | none | `SupportedLanguages` | Gets the current language |
| `getAvailableLanguages` | none | `{ code: SupportedLanguages, name: string, dir: LanguageDirection }[]` | Gets available languages |
| `get` | `key: string, params?: Record<string, string \| number>` | `Observable<string>` | Gets a translation as Observable |
| `instant` | `key: string, params?: Record<string, string \| number>` | `string` | Gets an instant translation |

#### Usage Example

```typescript
// Initialize translations
await this.translationService.initialize();

// Get current language
const currentLang = this.translationService.getCurrentLanguage();
console.log(`Current language: ${currentLang}`);

// Change language
this.translationService.changeLanguage('es').subscribe(() => {
  console.log('Language changed to Spanish');
});

// Get a translation
const welcomeMessage = this.translationService.instant('APP.WELCOME');
```

---

## Security Services

### SecurityService

Main service for application security features.

**Location:** `/src/app/shared/services/security/security.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `initialize` | none | `Promise<void>` | Initializes security features |
| `getCspNonce` | none | `string` | Gets Content Security Policy nonce |
| `validateCsrf` | `token: string` | `boolean` | Validates CSRF token |
| `generateCsrfToken` | none | `string` | Generates a new CSRF token |
| `getCsrfToken` | none | `string \| null` | Gets the current CSRF token |

#### Usage Example

```typescript
// Initialize security
await this.securityService.initialize();

// Get CSP nonce for inline scripts
const nonce = this.securityService.getCspNonce();
```

### CsrfService

Service for Cross-Site Request Forgery protection.

**Location:** `/src/app/shared/services/security/csrf.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `generateToken` | none | `string` | Generates a new CSRF token |
| `getToken` | none | `string \| null` | Gets the stored CSRF token |
| `validateToken` | `token: string` | `boolean` | Validates if token matches stored token |
| `getHeaderKey` | none | `string` | Gets the header key used for CSRF protection |

#### Usage Example

```typescript
// Generate a CSRF token
const token = this.csrfService.generateToken();

// Validate a token
const isValid = this.csrfService.validateToken(submittedToken);
```

### CsrfValidatorService

Service for validating CSRF tokens in requests.

**Location:** `/src/app/shared/services/security/csrf-validator.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `validateRequest` | `request: HttpRequest<unknown>` | `boolean` | Validates CSRF token in HTTP request |
| `shouldValidate` | `request: HttpRequest<unknown>` | `boolean` | Determines if request needs CSRF validation |

#### Usage Example

```typescript
// Check if request should be validated
if (this.csrfValidatorService.shouldValidate(request)) {
  // Validate the request
  const isValid = this.csrfValidatorService.validateRequest(request);
  if (!isValid) {
    throw new Error('Invalid CSRF token');
  }
}
```

---

## Utility Services

### LoggerService

Service for application logging.

**Location:** `/src/app/shared/services/logger/logger.service.ts`

#### Methods

| Name | Parameters | Return Type | Description |
|------|------------|-------------|-------------|
| `debug` | `message: string, context?: string, data?: any` | `void` | Logs debug message |
| `info` | `message: string, context?: string, data?: any` | `void` | Logs info message |
| `warn` | `message: string, context?: string, data?: any` | `void` | Logs warning message |
| `error` | `message: string, context?: string, error?: any` | `void` | Logs error message |
| `setLogLevel` | `level: LogLevel` | `void` | Sets the current log level |
| `getLogLevel` | none | `LogLevel` | Gets the current log level |

#### Usage Example

```typescript
// Log messages at different levels
this.logger.debug('Initializing component', 'MyComponent');
this.logger.info('User action completed', 'UserService', { userId: '123' });
this.logger.warn('Resource not found', 'ApiService');
this.logger.error('Failed to process request', 'DataService', error);

// Set log level
this.logger.setLogLevel(LogLevel.ERROR); // Only errors will be logged
```

---

## Type Reference

### Common Types

**Location:** `/src/app/shared/types/common.types.ts`

```typescript
/**
 * Generic interface for application entities
 */
export interface Entity {
  id: string;
  [key: string]: any;
}

/**
 * Generic interface for API responses
 */
export interface ApiResponse<T> {
  data: T;
  success: boolean;
  error?: string;
  meta?: Record<string, unknown>;
}

/**
 * Types for JSON data
 */
export type JsonPrimitive = string | number | boolean | null;
export type JsonValue = JsonPrimitive | JsonObject | JsonArray;
export interface JsonObject { [key: string]: JsonValue; }
export type JsonArray = JsonValue[];
```

### Diagram Types

**Location:** `/src/app/diagram/store/models/diagram.model.ts`

```typescript
/**
 * Diagram element types
 */
export enum DiagramElementType {
  RECTANGLE = 'rectangle',
  CIRCLE = 'circle',
  TRIANGLE = 'triangle',
  TEXT = 'text',
  LINE = 'line',
  IMAGE = 'image',
  CONNECTOR = 'connector'
}

/**
 * Diagram element interface
 */
export interface DiagramElement {
  id: string;
  type: DiagramElementType;
  position: Position;
  size: Size;
  properties: DiagramElementProperties;
  zIndex: number;
}

/**
 * Main diagram data interface
 */
export interface Diagram {
  id: string;
  name: string;
  elements: DiagramElement[];
  createdAt: string;
  updatedAt: string;
  version: number;
  properties: DiagramProperties;
}
```

### Authentication Types

**Location:** `/src/app/shared/services/auth/providers/auth-provider.interface.ts`

```typescript
/**
 * User information interface
 */
export interface UserInfo {
  id: string;
  name?: string;
  email?: string;
  picture?: string;
}

/**
 * Authentication provider interface
 */
export interface AuthProvider {
  login(): Promise<void>;
  logout(): Promise<void>;
  isAuthenticated(): boolean;
  getUserInfo(): UserInfo | null;
  silentSignIn(): Promise<boolean>;
}
```

### Storage Types

**Location:** `/src/app/shared/services/storage/providers/storage-provider.interface.ts`

```typescript
/**
 * Storage file interface
 */
export interface StorageFile {
  id: string;
  name: string;
  mimeType?: string;
  lastModified?: Date;
  size?: number;
  iconUrl?: string;
}

/**
 * Storage provider interface
 */
export interface StorageProvider {
  initialize(): Promise<boolean>;
  isInitialized(): boolean;
  createFile(name: string, data: string): Promise<StorageFile>;
  loadFile(fileId: string): Promise<string>;
  saveFile(fileId: string, data: string): Promise<void>;
  listFiles(): Promise<StorageFile[]>;
  showPicker(options: PickerOptions): Promise<PickerResult>;
}
```

---

## Extending the API

### Adding a New Service

To add a new service to the application:

1. Create a new file in the appropriate services directory
2. Define the service class with the `@Injectable()` decorator
3. Add necessary dependencies in the constructor
4. Implement the service methods
5. Add the service to the appropriate module providers or use `providedIn: 'root'`

### Best Practices

- Use TypeScript interfaces for all public API parameters and return types
- Add comprehensive JSDoc comments for all public methods
- Follow the existing patterns for error handling, logging, and state management
- Add unit tests for each service
- Update this documentation when adding or modifying APIs
# TMI Architecture Overview

This document provides an overview of the TMI application architecture, including its key components, design patterns, and data flow.

## System Architecture

TMI is a client-side Angular application that follows a modular, component-based architecture with standalone components. The application uses Angular's modern features, including Signals for reactive state management, for a streamlined and performant user experience.

![Architecture Diagram](https://via.placeholder.com/800x500?text=TMI+Architecture+Diagram)

### Key Components

The application is structured around these main architectural components:

1. **Core Application Shell** - Provides the overall application framework with standalone components
2. **Feature Routes** - Encapsulate specific functionality areas through lazy-loaded routes
3. **Shared Services** - Provide common functionality across features
4. **Signal-based State Services** - Manage application state using Angular Signals
5. **Provider Factories** - Create appropriate service implementations based on context
6. **API Interfaces** - Handle external data interactions

## Features and Routing

The application is organized into feature areas with standalone components and lazy-loaded routes:

### Landing Feature
- Entry point for users
- Provides marketing and informational pages
- Includes about, help, terms, privacy pages
- Loaded via lazy routes configuration

### Diagram Feature
- Core threat modeling functionality
- Canvas for creating and editing diagrams
- Context-aware toolbar with conditional controls
- Diagram home with welcome message when no diagram is loaded
- Diagram editor with canvas for active editing
- Export and sharing capabilities
- Signal-based state management
- Loaded via lazy routes configuration

### Shared Components and Services
- Reusable standalone components
- Authentication providers and services
- Storage providers and services
- Internationalization (i18n)
- Security services
- UI components (header, footer, file picker, etc.)
- Directives and pipes

## State Management

TMI uses Angular Signals for state management, providing a reactive, lightweight, and performant state management solution native to Angular. The application has been fully migrated from NgRx to Angular Signals.

### Signal-based State Management

The application state is managed through Signal-based state services:

1. **Core State Signals** - Represent the raw state of the application
2. **Computed Signals** - Derived state calculated from the core signals
3. **Effects** - Side effects that respond to signal changes
4. **Setter Methods** - Methods to update the state signals

### Signal Architecture

State signals are organized by feature area, with each feature having its own state service:

```typescript
@Injectable({
  providedIn: 'root'
})
export class DiagramStateService {
  // Core state signals
  readonly currentDiagram = signal<Diagram | null>(null);
  readonly elements = signal<DiagramElement[]>([]);
  readonly selectedElementId = signal<string | null>(null);
  
  // UI state signals
  readonly showGrid = signal<boolean>(true);
  readonly zoomLevel = signal<number>(1);
  
  // Computed signals
  readonly selectedElement = computed(() => {
    const id = this.selectedElementId();
    return this.elements().find(el => el.id === id) || null;
  });
  
  // Effects
  constructor() {
    effect(() => {
      // Synchronize data based on signal changes
    });
  }
  
  // State update methods
  addElement(...) { ... }
  updateElement(...) { ... }
  removeElement(...) { ... }
}
```

### State Interaction Pattern

With Angular Signals, components interact with the state following this simplified pattern:

1. **State Service** - Provides signals and methods to update state
2. **Component** - Injects the state service and uses signals directly in templates
3. **Template** - Binds directly to signals with automatic change detection
4. **Effects** - Automatically respond to state changes

## Data Flow

The application follows a unidirectional data flow pattern, simplified by Angular Signals:

![Data Flow Diagram](https://via.placeholder.com/800x300?text=Signal+Data+Flow+Diagram)

1. User interactions trigger events in components
2. Components call methods on state services
3. State services update signals
4. Computed signals automatically recalculate
5. Effects run in response to signal changes
6. Components automatically re-render when signals change

Benefits of this approach:
- Less boilerplate compared to traditional reactive state management
- Direct state access in templates without async pipe
- Automatic subscription management
- Better performance with fine-grained updates
- Simplified component code with less ceremony

## Authentication and Authorization

TMI implements a provider pattern for authentication, supporting multiple authentication methods:

1. **Google Authentication** - For users with Google accounts
2. **Anonymous Authentication** - For users without accounts (development and demo use)

### Authentication Provider Pattern

The application uses a factory pattern for authentication:

1. **Auth Provider Interface** - Defines the contract for all auth providers
2. **Auth Factory Service** - Creates the appropriate auth provider based on context
3. **Provider Implementations** - Concrete implementations for each auth method

Authentication flow:

1. User initiates login via the login button component
2. Auth service uses factory to get appropriate provider
3. Provider handles authentication flow
4. Auth service receives and validates credentials
5. User state is updated
6. Protected routes become accessible
7. Storage provider is selected based on auth provider

## Internationalization (i18n)

The application supports multiple languages using ngx-translate:

1. Translation keys in JSON files by language
2. Dynamic language switching
3. Right-to-left (RTL) language support
4. Automatic language detection based on browser settings

## Storage and Persistence

The application implements a provider pattern for storage, similar to authentication:

### Storage Provider Pattern

1. **Storage Provider Interface** - Defines the contract for all storage providers
2. **Storage Factory Service** - Creates the appropriate storage provider based on auth context
3. **Provider Implementations**:
   - **Google Drive Provider** - For users authenticated with Google
   - **Local Storage Provider** - For users using anonymous authentication

### Data Persistence Strategy

Diagram data is stored using the selected provider:

1. **Primary Storage** - Based on authentication method:
   - Google Drive for Google-authenticated users
   - Local storage for anonymously authenticated users
2. **Auto-save Functionality** - Periodically saves work using the selected provider
3. **Export Options** - For offline storage (JSON, PNG)

## Security Considerations

The application implements several security measures:

1. **CSRF Protection** - For API interactions
2. **Content Security Policy** - To prevent XSS attacks
3. **Safe HTML Handling** - For user-generated content
4. **Input Validation** - For all user inputs

## Diagram Engine

The diagram engine is built on [@maxgraph/core](https://github.com/maxGraph/maxGraph):

1. Canvas renderer for diagram elements
2. Interaction handlers for user manipulation
3. Custom shapes for threat modeling elements
4. Serialization/deserialization for storage

## Testing Strategy

The application follows a comprehensive testing approach using Cypress for both component and E2E testing:

1. **Component Tests** - Using Cypress Component Testing for UI components
2. **Integration Tests** - For feature interactions using Cypress
3. **E2E Tests** - For critical user journeys with full Cypress workflows

The project has been fully migrated from Jasmine/Karma to Cypress for all testing needs.

See the [Testing Guide](testing-guide.md) for more details.

## Deployment Architecture

The application can be deployed in various environments:

1. **Development** - Local development server
2. **Staging** - Pre-production environment
3. **Production** - Live environment

Deployment is automated through CI/CD pipelines that handle:

1. Building the application
2. Running tests
3. Deployment to the appropriate environment
4. Monitoring and logging

## UI Components and Styling

The application implements a consistent UI approach:

1. **Font Awesome Integration** - For icons throughout the application
2. **Roboto Condensed Font** - As the primary application font with appropriate fallbacks
3. **Conditional UI Rendering** - Based on application state (e.g., diagram toolbar)
4. **Responsive Design** - For various device sizes and orientations

### Icon System

The application uses Font Awesome icons with selective importing:
- Solid icons for most UI elements
- Regular icons for secondary UI elements
- Brand icons for third-party integrations (Google, etc.)

### Typography

- **Primary Font**: Roboto Condensed
- **Fallback Fonts**: Sans-serif fonts appropriate for each platform
- **Consistent sizing**: Using relative units (rem/em) for all text

## Performance Considerations

The application implements several performance optimizations:

1. **Standalone Components** - Improved tree-shaking and bundle optimization
2. **Lazy Loading** - Feature routes loaded on demand
3. **OnPush Change Detection** - Optimized rendering
4. **Signal-based State** - Fine-grained reactivity with minimal overhead
5. **Memoized Functions** - For expensive operations
6. **Asset Optimization** - For faster loading
7. **Selective Icon Importing** - Only importing required Font Awesome icons
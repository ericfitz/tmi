# TMI Architecture Overview

This document provides an overview of the TMI application architecture, including its key components, design patterns, and data flow.

## System Architecture

TMI is a client-side Angular application that follows a modular, component-based architecture with standalone components. The application uses a combination of Angular's standard patterns and NgRx for state management, employing the Facade pattern to simplify component-store interactions.

![Architecture Diagram](https://via.placeholder.com/800x500?text=TMI+Architecture+Diagram)

### Key Components

The application is structured around these main architectural components:

1. **Core Application Shell** - Provides the overall application framework with standalone components
2. **Feature Routes** - Encapsulate specific functionality areas through lazy-loaded routes
3. **Shared Services** - Provide common functionality across features
4. **Facade Services** - Abstract NgRx store interactions from components
5. **NgRx Store** - Manages application state
6. **Provider Factories** - Create appropriate service implementations based on context
7. **API Interfaces** - Handle external data interactions

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
- Facade service for state management
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

TMI uses NgRx for state management, providing a predictable state container based on the Redux pattern, with the Facade pattern to simplify interactions.

### Store Structure

The application state is organized by feature area:

```
{
  router: { ... },           // Router state
  auth: { ... },             // Authentication state
  diagrams: {                // Diagrams feature state
    ids: [ ... ],            // Entity IDs using @ngrx/entity
    entities: { ... },       // Normalized entities
    selectedId: string,      // Currently selected diagram
    loading: boolean,        // Loading status
    error: string | null     // Error message if any
  }
}
```

### Facade Pattern Implementation

The application uses the Facade pattern to abstract NgRx store interactions from components:

1. **Facade Services** - Provide a simplified API for components to interact with the store
2. **Component Interaction** - Components only interact with Facade services, not directly with the store
3. **Side Effect Handling** - Facades handle side effects (API calls, complex operations) that previously required NgRx Effects
4. **Single Responsibility** - Components focus on presentation, Facades handle state management logic

### State Interaction Pattern

With the Facade pattern, components interact with the state following this simplified pattern:

1. **Component** - Calls facade methods and subscribes to facade observables
2. **Facade** - Dispatches actions to the store and exposes state via observables
3. **Actions** - Describe state changes
4. **Reducers** - Apply state changes
5. **Selectors** - Extract state (used internally by Facades)

## Data Flow

The application follows a unidirectional data flow pattern, simplified by the Facade pattern:

![Data Flow Diagram](https://via.placeholder.com/800x300?text=Data+Flow+Diagram)

1. User interactions trigger events in components
2. Components call methods on Facade services
3. Facades dispatch actions to the store and handle side effects
4. Reducers process actions and update state
5. Facades expose the updated state via observables
6. Components receive updated state via Facade observables
7. Components re-render based on new state

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
5. User state is updated in the store
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

The application follows a comprehensive testing approach:

1. **Unit Tests** - For components, services, reducers, etc.
2. **Integration Tests** - For feature interactions
3. **E2E Tests** - For critical user journeys

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
4. **Facade Pattern** - Simplified component architecture with less overhead
5. **Memoized Selectors** - To prevent unnecessary calculations
6. **Asset Optimization** - For faster loading
7. **Selective Icon Importing** - Only importing required Font Awesome icons
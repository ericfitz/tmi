# TMI Project TODOs

This document outlines tasks that need to be addressed to improve the project's quality and maintainability.

## Type Safety Improvements

- [x] Replace `any` types in diagram-canvas.component.ts with proper types
- [x] Replace `any` types in diagram-toolbar.component.ts with proper types
- [x] Replace `any` types in diagram.service.ts with proper types
- [x] Replace `any` types in diagram.effects.ts with proper types
- [x] Replace `any` types in google-auth.provider.ts with proper types
- [x] Replace `any` types in translation.service.ts with proper types
- [x] Replace `any` types in google-storage.provider.ts with proper types
- [x] Create types for external API responses to avoid `any`
- [ ] Enhance the JsonObject and JsonValue types with more specific subtypes

## Linting & Formatting

- [x] Fix the remaining function return type warnings in app.config.ts (factory functions)
- [x] Fix the function return type warnings in diagram.selectors.ts
- [x] Address the empty arrow function warning in csrf-init.interceptor.ts
- [x] Rename class properties in csrf.service.ts to follow camelCase convention

## Testing

- [x] Implement e2e tests using Cypress (already migrated from Protractor)
- [x] Fix TypeScript errors in existing tests
- [ ] Improve test coverage for diagram-toolbar component
- [ ] Add more unit tests for diagram state management
- [ ] Add more unit tests for authentication service
- [ ] Add integration tests for diagram editing workflow

## Documentation

- [x] Add API documentation for all core services (created comprehensive api-documentation.md)
- [ ] Replace placeholder diagrams with actual component diagrams in architecture.md
- [x] Add examples of typical diagram workflows to docs (covered in architecture.md)
- [ ] Document the error handling approach
- [x] Improve TypeScript interface documentation (added to api-documentation.md)

## Performance Optimization

- [x] Optimize the diagram rendering with OnPush change detection
- [x] Add memoization to heavy computations in diagram canvas
- [x] Optimize NgRx selectors with createSelector memoization
- [ ] Use trackBy functions for all ngFor directives
- [ ] Review and optimize bundle size

## Accessibility

- [ ] Add aria attributes to diagram components
- [ ] Improve keyboard navigation in the diagram editor
- [ ] Add screen reader support for diagram elements
- [ ] Improve color contrast for visual elements
- [ ] Add alt text to all icons and graphical elements

## Setup Additional Quality Tools

- [ ] Configure pre-commit hooks to run linting and tests
- [ ] Set up CI/CD pipeline for automated testing and deployment
- [ ] Add bundle analyzer to monitor package size
- [ ] Configure code coverage thresholds
- [ ] Add visual regression testing for component rendering

## Refactoring

- [x] Extract diagram rendering logic into a separate service
- [x] Refactor state management to use NgRx facades
- [ ] Reduce component complexity in diagram-canvas
- [x] Improve separation of concerns in authentication logic
- [ ] Apply consistent error handling patterns

## Dependency Cleanup

- [x] Remove unused @ngrx/effects package from package.json
- [x] Convert IconModule to a standalone service or function for Font Awesome icon registration
- [x] Convert DiagramStoreModule to direct imports in app.config.ts
- [ ] Remove SharedModule and use standalone component imports throughout the application
- [ ] Update tests to use standalone component testing approach instead of NgModule
- [ ] Clean up any remaining module-based imports that are no longer needed
- [ ] Update any remaining module-specific code to use standalone pattern

## Feature Additions

- [ ] Add diagram export functionality
- [ ] Implement collaborative editing features
- [ ] Create a diagram template system
- [ ] Add diagram versioning
- [ ] Support additional diagram element types
# TMI Component Guide

This document provides guidelines for creating and working with components in the TMI application.

## Component Structure

All components in TMI should follow this standard structure:

### File Organization

Components should be organized in feature folders, with each component having its own folder containing:

```
component-name/
├── component-name.component.ts
├── component-name.component.html
├── component-name.component.scss
└── component-name.component.spec.ts
```

### Component Class Structure

The TypeScript class for a component should follow this structure:

1. **Imports** - Ordered alphabetically
2. **Component Decorator** - With selector, templateUrl, styleUrls
3. **Class Properties** - Organized by:
   - Constants
   - Inputs
   - Outputs
   - Public properties
   - Private properties
4. **Constructor**
5. **Lifecycle Methods** - In order of execution (ngOnInit, ngAfterViewInit, etc.)
6. **Public Methods**
7. **Private Methods**

Example:

```typescript
import { Component, EventEmitter, Input, OnInit, Output } from '@angular/core';
import { SomeService } from '../../services/some.service';

@Component({
  selector: 'app-example',
  templateUrl: './example.component.html',
  styleUrls: ['./example.component.scss']
})
export class ExampleComponent implements OnInit {
  // Constants
  readonly MAX_ITEMS = 10;
  
  // Inputs
  @Input() data: SomeData[] = [];
  @Input() title = 'Default Title';
  
  // Outputs
  @Output() itemSelected = new EventEmitter<SomeData>();
  
  // Public properties
  selectedItem: SomeData | null = null;
  
  // Private properties
  private _isLoading = false;
  
  constructor(private someService: SomeService) {}
  
  // Lifecycle methods
  ngOnInit(): void {
    this.initializeData();
  }
  
  // Public methods
  onItemClick(item: SomeData): void {
    this.selectedItem = item;
    this.itemSelected.emit(item);
  }
  
  // Private methods
  private initializeData(): void {
    // Implementation
  }
}
```

## Component Templates

Templates should follow these guidelines:

1. Use structural directives (*ngIf, *ngFor) on container elements
2. Always use the async pipe for Observables
3. Keep templates clean with presentation logic only
4. Always use translate pipes for user-visible text
5. Use trackBy functions with *ngFor for better performance

Example:

```html
<div class="example-component">
  <h2>{{ title | translate }}</h2>
  
  <div *ngIf="(dataLoaded$ | async); else loading">
    <ul>
      <li *ngFor="let item of data; trackBy: trackById" 
          [class.selected]="item === selectedItem"
          (click)="onItemClick(item)">
        {{ item.name }}
      </li>
    </ul>
  </div>
  
  <ng-template #loading>
    <app-spinner></app-spinner>
  </ng-template>
</div>
```

## Component Styles

Styling guidelines:

1. Use BEM (Block, Element, Modifier) naming convention
2. Use CSS variables for colors, spacing, and typography
3. Include RTL support for all components
4. Avoid element selectors, prefer class selectors
5. Use nesting sparingly to avoid specificity issues

Example:

```scss
.example-component {
  margin: var(--spacing-medium);
  
  &__header {
    color: var(--color-primary);
    font-size: var(--font-size-large);
  }
  
  &__list {
    list-style: none;
    padding: 0;
  }
  
  &__item {
    padding: var(--spacing-small);
    border-bottom: 1px solid var(--color-border);
    
    &--selected {
      background-color: var(--color-accent-light);
    }
    
    // RTL support
    [dir="rtl"] & {
      padding-right: var(--spacing-medium);
      padding-left: 0;
    }
  }
}
```

## Component Testing

Each component should have a comprehensive test suite that:

1. Tests component creation
2. Tests input binding
3. Tests output events
4. Tests component behavior
5. Tests template rendering

See the [Testing Guide](testing-guide.md) for detailed examples.

## Component Documentation

Each component should include JSDoc comments:

```typescript
/**
 * A component that displays and manages a list of selectable items.
 * 
 * @example
 * <app-example
 *   [data]="items"
 *   [title]="'My Items'"
 *   (itemSelected)="onSelection($event)">
 * </app-example>
 */
@Component({...})
export class ExampleComponent {
  /**
   * The items to display in the list
   */
  @Input() data: SomeData[] = [];
  
  /**
   * Emitted when an item is selected from the list
   */
  @Output() itemSelected = new EventEmitter<SomeData>();
  
  /**
   * Handles user click on an item
   * @param item The item that was clicked
   */
  onItemClick(item: SomeData): void {
    // Implementation
  }
}
```

## Smart vs. Presentational Components

The application uses a distinction between smart (container) components and presentational components:

### Smart Components

- Connect to services and state management
- Pass data to child components
- Handle events from child components
- Coordinate application logic
- Located in feature folders

### Presentational Components

- Accept data via inputs
- Emit events via outputs
- Focus on the UI presentation
- Don't depend on services
- Reusable across features
- Located in shared/components when used by multiple features

## Accessibility

All components must be accessible:

1. Use semantic HTML elements
2. Include proper ARIA attributes
3. Ensure keyboard navigation works
4. Maintain sufficient color contrast
5. Support screen readers
6. Support mobile and touch devices

## Performance Considerations

To ensure good performance:

1. Use OnPush change detection for pure components
2. Implement trackBy for all ngFor directives
3. Use async pipe rather than manual subscription
4. Avoid complex computations in templates
5. Lazy load feature modules

## State Management

For components that need to interact with global state:

1. Use the NgRx store for global state management
2. Use selectors to query state
3. Use actions to update state
4. Don't manipulate state directly from components
5. Use effects for side-effects like API calls
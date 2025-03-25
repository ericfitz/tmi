# TMI Type Safety Guide

This document provides guidelines for maintaining type safety in the TMI application.

## Type Safety Principles

TMI follows these principles for type safety:

1. **No explicit `any` types**: Avoid using the `any` type whenever possible
2. **Strong typing for all variables and parameters**: Use explicit types for all variables and function parameters
3. **Use of generic types**: Leverage TypeScript generics for reusable, type-safe components
4. **Custom type definitions**: Create custom types for domain-specific concepts
5. **Type guards for runtime checking**: Use type guards when runtime type information is needed

## Common Types

The application defines several common types that should be used in place of `any`:

```typescript
// For generic objects
interface JsonObject {
  [key: string]: JsonValue;
}

// For JSON-compatible values
type JsonValue = 
  | string
  | number
  | boolean
  | null
  | JsonObject
  | JsonValue[];

// For error objects
interface ErrorResponse {
  message: string;
  code?: string;
  status?: number;
  details?: Record<string, unknown>;
}

// For unknown values (safer than any)
function processData(data: unknown): void {
  // Use type guards to narrow the type
  if (typeof data === 'string') {
    // Now TypeScript knows data is a string
    console.log(data.toUpperCase());
  }
}

// Dictionary type for key-value pairs
interface Dictionary<T> {
  [key: string]: T;
}
```

## Using the Unknown Type

The `unknown` type is safer than `any` and should be preferred when the exact type is not known:

```typescript
// Bad: Using any
function parseData(data: any): void {
  console.log(data.length); // No error, but might crash at runtime
}

// Good: Using unknown with type guard
function parseData(data: unknown): void {
  if (Array.isArray(data)) {
    console.log(data.length); // Safe
  }
}
```

## Type Assertions

Use type assertions sparingly and only when you are certain of the type:

```typescript
// Only use when you're certain of the type
const element = document.getElementById('myElement') as HTMLInputElement;
```

Prefer type guards when possible:

```typescript
const element = document.getElementById('myElement');
if (element instanceof HTMLInputElement) {
  // Now TypeScript knows element is an HTMLInputElement
  element.value = 'New value';
}
```

## Type Guards

Create custom type guards for domain-specific types:

```typescript
// Type guard for DiagramElement
function isDiagramElement(obj: unknown): obj is DiagramElement {
  return (
    typeof obj === 'object' && 
    obj !== null && 
    'id' in obj && 
    'type' in obj && 
    'position' in obj
  );
}

// Usage
function processElement(element: unknown): void {
  if (isDiagramElement(element)) {
    // TypeScript now knows element is a DiagramElement
    console.log(element.id, element.type);
  }
}
```

## Generic Components

Use generic types for components that work with different data types:

```typescript
// Generic data list component
interface DataListProps<T> {
  items: T[];
  renderItem: (item: T) => React.ReactNode;
}

function DataList<T>(props: DataListProps<T>): React.ReactElement {
  return (
    <div>
      {props.items.map((item, index) => (
        <div key={index}>{props.renderItem(item)}</div>
      ))}
    </div>
  );
}
```

## NgRx Type Safety

For NgRx state management, define strongly typed state interfaces:

```typescript
// Define state interface
export interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  error: ErrorResponse | null;
  loading: boolean;
}

// Define initial state with the interface
export const initialState: AuthState = {
  user: null,
  isAuthenticated: false,
  error: null,
  loading: false
};

// Use strongly typed selectors
export const selectUser = createSelector(
  selectAuthState,
  (state: AuthState) => state.user
);
```

## Handling External APIs

When working with external APIs that don't provide type definitions:

1. Create your own type definitions
2. Use the `unknown` type initially
3. Add type guards or type assertions
4. Consider runtime validation with libraries like Zod or io-ts

```typescript
// Define the shape of the API response
interface ApiResponse {
  data: {
    id: string;
    attributes: {
      name: string;
      status: 'active' | 'inactive';
    };
  }[];
}

// Validate and narrow the type
function processApiResponse(response: unknown): ApiResponse['data'] {
  if (
    !response ||
    typeof response !== 'object' ||
    !('data' in response) ||
    !Array.isArray((response as any).data)
  ) {
    throw new Error('Invalid API response');
  }
  
  // Now we can safely assert the type
  return (response as ApiResponse).data;
}
```

## Dealing with Libraries without Types

For libraries without TypeScript definitions:

1. Check if types are available from DefinitelyTyped (@types/library-name)
2. Create your own type definitions in a declaration file

```typescript
// legacy-library.d.ts
declare module 'legacy-library' {
  export function doSomething(value: string): number;
  export class Widget {
    constructor(element: HTMLElement);
    update(data: any): void; // Use any when you really can't be more specific
  }
}
```

## Type Safety Tools

The project uses several tools to enforce type safety:

1. **ESLint with TypeScript rules**: Prevents usage of `any` and enforces type safety
2. **Strict TypeScript config**: Enables strict mode for maximum type checking
3. **Pre-commit hooks**: Validates type safety before commit
4. **CI checks**: Ensures all code merged to main branches maintains type safety

## Progressive Type Safety Improvement

For existing code with `any` types:

1. Identify code using `any` with `grep -r "any" --include="*.ts" src/`
2. Prioritize high-risk or frequently used components
3. Add .eslintignore entries for files that can't be immediately fixed
4. Replace `any` with more specific types or `unknown`
5. Add test coverage to ensure type safety at runtime

## Resources

- [TypeScript Handbook](https://www.typescriptlang.org/docs/handbook/intro.html)
- [TypeScript Deep Dive](https://basarat.gitbook.io/typescript/)
- [Total TypeScript](https://www.totaltypescript.com/)
- [Type Challenges](https://github.com/type-challenges/type-challenges)
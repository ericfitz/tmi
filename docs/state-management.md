# TMI State Management Guide

This guide explains the state management approach used in the TMI application using NgRx.

## Overview

TMI uses NgRx as its primary state management solution. NgRx provides a Redux-inspired state management pattern that emphasizes:

- A single source of truth (the store)
- Immutable state
- Pure function reducers
- Unidirectional data flow
- Actions as the drivers of state changes

![NgRx Flow Diagram](https://via.placeholder.com/800x400?text=NgRx+Flow+Diagram)

## Key Concepts

### Store

The centralized state container that holds the entire application state as a single immutable object.

### Actions

Events that describe state changes. Actions have:
- A type identifier (e.g., `[Diagram] Load Diagrams`)
- An optional payload containing data

### Reducers

Pure functions that:
- Take the current state and an action
- Return a new state object
- Never mutate the existing state

### Selectors

Functions that extract specific slices of state from the store, with optional memoization for performance.

### Effects

Side-effect handling for asynchronous operations (API requests, localStorage, etc.) that:
- Listen for specific actions
- Perform side effects
- Dispatch new actions in response

## Store Structure

The TMI store is organized by feature area:

```typescript
export interface AppState {
  router: RouterReducerState<RouterStateUrl>;
  auth: AuthState;
  diagrams: DiagramState;
}
```

Each feature state has its own structure. For example, the diagram state:

```typescript
export interface DiagramState extends EntityState<Diagram> {
  selectedId: string | null;
  loading: boolean;
  error: string | null;
  filter: DiagramFilter | null;
}
```

## Code Organization

NgRx code is organized by feature, with consistent file structure:

```
feature/
├── store/
│   ├── actions/
│   │   └── feature.actions.ts
│   ├── effects/
│   │   └── feature.effects.ts
│   ├── models/
│   │   └── feature.model.ts
│   ├── reducers/
│   │   └── feature.reducer.ts
│   ├── selectors/
│   │   └── feature.selectors.ts
│   └── feature-store.module.ts
```

## Actions

Actions follow a consistent naming convention:

```typescript
export const loadDiagrams = createAction(
  '[Diagram] Load Diagrams'
);

export const loadDiagramsSuccess = createAction(
  '[Diagram] Load Diagrams Success',
  props<{ diagrams: Diagram[] }>()
);

export const loadDiagramsFailure = createAction(
  '[Diagram] Load Diagrams Failure',
  props<{ error: string }>()
);
```

Each action name has three parts:
1. Source feature in brackets (e.g., `[Diagram]`)
2. Description of the event (e.g., `Load Diagrams`)
3. Optional result qualifier (e.g., `Success`, `Failure`)

## Reducers

Reducers are implemented using NgRx's `createReducer` and `on` functions:

```typescript
export const initialState: DiagramState = diagramAdapter.getInitialState({
  selectedId: null,
  loading: false,
  error: null,
  filter: null
});

export const diagramReducer = createReducer(
  initialState,
  
  on(DiagramActions.loadDiagrams, state => ({
    ...state,
    loading: true,
    error: null
  })),
  
  on(DiagramActions.loadDiagramsSuccess, (state, { diagrams }) => 
    diagramAdapter.setAll(diagrams, {
      ...state,
      loading: false
    })
  ),
  
  on(DiagramActions.loadDiagramsFailure, (state, { error }) => ({
    ...state,
    loading: false,
    error
  }))
);
```

## Entity Adapter

For collections of items, we use NgRx Entity to standardize CRUD operations:

```typescript
export const diagramAdapter = createEntityAdapter<Diagram>({
  selectId: model => model.id,
  sortComparer: (a, b) => a.name.localeCompare(b.name)
});
```

## Selectors

Selectors are organized from general to specific:

```typescript
// Feature selector
export const selectDiagramState = createFeatureSelector<DiagramState>('diagrams');

// Entity adapter selectors
export const {
  selectIds: selectDiagramIds,
  selectEntities: selectDiagramEntities,
  selectAll: selectAllDiagrams,
  selectTotal: selectTotalDiagrams
} = diagramAdapter.getSelectors(selectDiagramState);

// Specific selectors
export const selectDiagramLoading = createSelector(
  selectDiagramState,
  state => state.loading
);

export const selectSelectedDiagramId = createSelector(
  selectDiagramState,
  state => state.selectedId
);

export const selectSelectedDiagram = createSelector(
  selectDiagramEntities,
  selectSelectedDiagramId,
  (diagramEntities, selectedId) => 
    selectedId ? diagramEntities[selectedId] : null
);

// Derived data selector
export const selectElementsByType = createSelector(
  selectSelectedDiagram,
  (diagram, props: { type: ElementType }) => 
    diagram?.elements.filter(el => el.type === props.type) || []
);
```

## Effects

Effects handle asynchronous operations and side-effects:

```typescript
@Injectable()
export class DiagramEffects {
  loadDiagrams$ = createEffect(() => this.actions$.pipe(
    ofType(DiagramActions.loadDiagrams),
    switchMap(() => 
      this.diagramService.getAllDiagrams().pipe(
        map(diagrams => DiagramActions.loadDiagramsSuccess({ diagrams })),
        catchError(error => of(DiagramActions.loadDiagramsFailure({ 
          error: error.message 
        })))
      )
    )
  ));

  constructor(
    private actions$: Actions,
    private diagramService: DiagramService
  ) {}
}
```

## Using Store in Components

Components interact with the store by:
1. Dispatching actions
2. Selecting state with selectors

```typescript
@Component({
  selector: 'app-diagram-list',
  templateUrl: './diagram-list.component.html'
})
export class DiagramListComponent implements OnInit {
  diagrams$ = this.store.select(selectAllDiagrams);
  loading$ = this.store.select(selectDiagramLoading);
  error$ = this.store.select(state => state.diagrams.error);

  constructor(private store: Store<AppState>) {}

  ngOnInit(): void {
    this.store.dispatch(DiagramActions.loadDiagrams());
  }

  onSelectDiagram(id: string): void {
    this.store.dispatch(DiagramActions.selectDiagram({ id }));
  }
}
```

## Best Practices

### Action Design
- Keep actions fine-grained and specific
- Use descriptive action types
- Include only necessary data in payloads
- Group related actions in separate files for large features

### Reducer Design
- Keep reducers pure and simple
- Handle each action in the most straightforward way
- Use spread operator for immutable updates
- Use NgRx Entity adapters for collections

### Selector Design
- Create dedicated selectors for each component's data needs
- Compose selectors for derived data
- Use memoization for expensive calculations
- Keep UI-specific transformations in the component

### Effect Design
- Focus each effect on a single responsibility
- Properly handle errors
- Use appropriate RxJS operators (switchMap, mergeMap, concatMap)
- Consider cancellation for long-running operations

### Testing
- Test each part of the NgRx system in isolation:
  - Actions (verify structure)
  - Reducers (verify state transitions)
  - Selectors (verify data extraction)
  - Effects (verify action streams)

## Advanced Patterns

### Optimistic Updates
For better user experience, implement optimistic updates by:
1. Dispatching success action immediately
2. Updating the state as if operation succeeded
3. Reverting if operation fails

### Custom Action Creators
For common patterns, create custom action creators:

```typescript
export function createApiActions<T>(feature: string, name: string) {
  return {
    request: createAction(
      `[${feature}] ${name} Request`,
      props<{ params?: any }>()
    ),
    success: createAction(
      `[${feature}] ${name} Success`,
      props<{ result: T }>()
    ),
    failure: createAction(
      `[${feature}] ${name} Failure`,
      props<{ error: any }>()
    )
  };
}
```

### State Preloading
For critical data, preload state during app initialization:

```typescript
@Injectable()
export class AppInitEffects {
  init$ = createEffect(() => this.actions$.pipe(
    ofType(ROOT_EFFECTS_INIT),
    mergeMap(() => [
      AuthActions.checkAuth(),
      DiagramActions.loadDiagrams()
    ])
  ));

  constructor(private actions$: Actions) {}
}
```

## Resources

- [NgRx Documentation](https://ngrx.io/)
- [Redux DevTools Extension](https://github.com/reduxjs/redux-devtools)
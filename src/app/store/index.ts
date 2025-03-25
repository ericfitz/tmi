import { ActionReducerMap, MetaReducer } from '@ngrx/store';
import { environment } from '../../environments/environment';
import * as fromDiagram from '../diagram/store/reducers/diagram.reducer';

/**
 * Root state interface that combines all feature states
 */
export interface AppState {
  diagram: fromDiagram.DiagramState;
  // Add other feature states here as they're created
}

/**
 * Root reducer map that combines all feature reducers
 */
export const reducers: ActionReducerMap<AppState> = {
  diagram: fromDiagram.diagramReducer,
  // Add other feature reducers here as they're created
};

/**
 * Root meta-reducers (for logging, debugging, etc.)
 */
export const metaReducers: MetaReducer<AppState>[] = !environment.production
  ? [] // Add development meta-reducers here (e.g. logger, storeFreeze)
  : [];
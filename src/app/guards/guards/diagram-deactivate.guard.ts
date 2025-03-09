import { CanDeactivateFn } from '@angular/router';

export const diagramDeactivateGuard: CanDeactivateFn<unknown> = (component, currentRoute, currentState, nextState) => {
  return true;
};

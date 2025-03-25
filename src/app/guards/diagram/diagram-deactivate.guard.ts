import { Injectable } from '@angular/core';
import { CanDeactivate } from '@angular/router';
import { TranslateService } from '@ngx-translate/core';
import { DiagramService } from '../../diagram/services/diagram.service';

/**
 * Guard to prevent navigating away from the diagram editor with unsaved changes
 */
@Injectable({
  providedIn: 'root'
})
export class DiagramDeactivateGuard implements CanDeactivate<any> {
  constructor(
    private diagramService: DiagramService,
    private translate: TranslateService
  ) {}

  canDeactivate(): boolean {
    // Check if there are unsaved changes
    if (this.diagramService.isDiagramDirty()) {
      // Prompt the user to confirm leaving
      return confirm(this.translate.instant('DIAGRAM.DEACTIVATE_GUARD.UNSAVED_CHANGES'));
    }
    
    // No unsaved changes, allow navigation
    return true;
  }
}
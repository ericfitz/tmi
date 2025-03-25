import { Component, OnInit, OnDestroy, ChangeDetectionStrategy } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { Observable, Subscription, firstValueFrom } from 'rxjs';
import { take } from 'rxjs/operators';
import { DiagramCanvasComponent } from './diagram-canvas/diagram-canvas.component';
import { DiagramToolbarComponent } from './diagram-toolbar/diagram-toolbar.component';
import { AsyncPipe, NgIf, CommonModule } from '@angular/common';
import { Diagram } from './store/models/diagram.model';
import { ErrorResponse } from '../shared/types/common.types';
import { DiagramFacadeService } from './services/diagram-facade.service';
// Removed unused TranslatePipe import

@Component({
  selector: 'app-diagram',
  standalone: true,
  imports: [DiagramCanvasComponent, DiagramToolbarComponent, AsyncPipe, NgIf, CommonModule],
  templateUrl: './diagram.component.html',
  styleUrl: './diagram.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class DiagramComponent implements OnInit, OnDestroy {
  diagram$: Observable<Diagram | null>;
  isLoading$: Observable<boolean>;
  error$: Observable<ErrorResponse | null>;
  
  private subscriptions: Subscription[] = [];

  constructor(
    private diagramFacade: DiagramFacadeService,
    private route: ActivatedRoute,
    private router: Router
  ) {
    this.diagram$ = this.diagramFacade.currentDiagram$;
    this.isLoading$ = this.diagramFacade.isLoading$;
    this.error$ = this.diagramFacade.error$;
  }

  ngOnInit(): void {
    // Check for diagram ID in route params
    this.subscriptions.push(
      this.route.params.subscribe(async params => {
        const diagramId = params['id'];
        if (diagramId) {
          // Load specific diagram
          await this.diagramFacade.loadDiagram(diagramId);
        } else {
          // Create a new diagram
          await this.createNewDiagram();
        }
      })
    );
  }

  ngOnDestroy(): void {
    // Clean up subscriptions
    this.subscriptions.forEach(sub => sub.unsubscribe());
    
    // Clear current diagram when leaving
    this.diagramFacade.clearDiagram();
  }

  /**
   * Create a new diagram if no ID is provided
   */
  private async createNewDiagram(): Promise<void> {
    try {
      // Check if we already have a diagram loaded
      const diagram = await firstValueFrom(this.diagram$.pipe(take(1)));
      
      if (!diagram) {
        await this.diagramFacade.createDiagram('Untitled Diagram', {
          backgroundColor: '#ffffff',
          gridSize: 20,
          snapToGrid: true
        });
      }
    } catch (error) {
      console.error('Error creating new diagram:', error);
    }
  }

  /**
   * Save the current diagram
   */
  saveDiagram(): void {
    this.diagramFacade.saveDiagram();
  }

  /**
   * Undo the last action
   */
  undo(): void {
    this.diagramFacade.undo();
  }

  /**
   * Redo the last undone action
   */
  redo(): void {
    this.diagramFacade.redo();
  }
  
  /**
   * Close the current diagram
   */
  closeDiagram(): void {
    // Clear the current diagram
    this.diagramFacade.clearDiagram();
    
    // Reload the current route to show the welcome message
    this.router.navigateByUrl('/diagrams');
  }
}
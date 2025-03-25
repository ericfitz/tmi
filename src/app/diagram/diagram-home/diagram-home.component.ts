import { 
  Component, 
  OnInit, 
  OnDestroy, 
  ChangeDetectionStrategy,
  ViewChild
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { Router } from '@angular/router';
import { Observable, Subscription } from 'rxjs';
import { map } from 'rxjs/operators';
import { CdkVirtualScrollViewport, ScrollingModule } from '@angular/cdk/scrolling';
import { DiagramMetadata } from '../store/models/diagram.model';
import { DiagramFacadeService } from '../services/diagram-facade.service';
import { TranslatePipe } from '../../shared/pipes/translate.pipe';
import { DiagramToolbarComponent } from '../diagram-toolbar/diagram-toolbar.component';
import { LoggerService } from '../../shared/services/logger/logger.service';

@Component({
  selector: 'app-diagram-home',
  standalone: true,
  imports: [CommonModule, ScrollingModule, TranslatePipe, DiagramToolbarComponent],
  templateUrl: './diagram-home.component.html',
  styleUrls: ['./diagram-home.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class DiagramHomeComponent implements OnInit, OnDestroy {
  @ViewChild(CdkVirtualScrollViewport) viewport!: CdkVirtualScrollViewport;
  
  // Observables for the template
  diagrams$: Observable<DiagramMetadata[]>;
  isLoading$: Observable<boolean>;
  isEmpty$: Observable<boolean>;
  
  // Track subscriptions for cleanup
  private subscriptions: Subscription[] = [];
  
  // Item size for virtual scrolling (in pixels)
  readonly itemSize = 80;

  constructor(
    private diagramFacade: DiagramFacadeService,
    private router: Router,
    private logger: LoggerService
  ) {
    this.diagrams$ = this.diagramFacade.diagramList$;
    this.isLoading$ = this.diagramFacade.isLoading$;
    this.isEmpty$ = this.diagrams$.pipe(
      map(diagrams => diagrams.length === 0)
    );
  }

  ngOnInit(): void {
    // Load the diagram list
    this.diagramFacade.loadDiagramList();
  }

  ngOnDestroy(): void {
    // Clean up subscriptions
    this.subscriptions.forEach(sub => sub.unsubscribe());
  }

  /**
   * Create a new diagram
   */
  async createNewDiagram(): Promise<void> {
    try {
      const result = await this.diagramFacade.createDiagram('Untitled Diagram', {
        backgroundColor: '#ffffff',
        gridSize: 20,
        snapToGrid: true
      });
      
      // Only navigate to editor if diagram creation was successful
      if (result && result.success) {
        this.router.navigate(['/diagrams/editor']);
      } else {
        // Stay on the current page and let the error handler show the error
        this.logger.warn('Failed to create new diagram - staying on current page', 'DiagramHomeComponent');
      }
    } catch (error) {
      this.logger.error('Failed to create new diagram:', 'DiagramHomeComponent', error);
      // Error state should be handled by the facade, we stay on current page
    }
  }

  /**
   * Open a diagram
   */
  openDiagram(diagram: DiagramMetadata): void {
    this.router.navigate(['/diagrams/editor', diagram.id]);
  }

  /**
   * Track by function for ngFor with virtual scrolling
   */
  trackByFn(index: number, item: DiagramMetadata): string {
    return item.id;
  }
}
import { 
  Component, 
  OnInit, 
  ChangeDetectionStrategy,
  ViewChild,
  computed,
  inject
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { Router } from '@angular/router';
import { CdkVirtualScrollViewport, ScrollingModule } from '@angular/cdk/scrolling';
import { DiagramMetadata } from '../models/diagram.model';
import { DiagramStateService } from '../services/diagram-state.service';
import { TranslatePipe } from '../../shared/pipes/translate.pipe';
import { DiagramToolbarComponent } from '../diagram-toolbar/diagram-toolbar.component';
import { LoggerService } from '../../shared/services/logger/logger.service';

@Component({
  selector: 'app-diagram-home',
  standalone: true,
  imports: [CommonModule, ScrollingModule, DiagramToolbarComponent, TranslatePipe],
  templateUrl: './diagram-home.component.html',
  styleUrls: ['./diagram-home.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class DiagramHomeComponent implements OnInit {
  @ViewChild(CdkVirtualScrollViewport) viewport!: CdkVirtualScrollViewport;
  
  // Inject services
  public diagramState = inject(DiagramStateService);
  private router = inject(Router);
  private logger = inject(LoggerService);
  
  // Item size for virtual scrolling (in pixels)
  readonly itemSize = 80;
  
  // Computed signal for empty state
  readonly isEmpty = computed(() => this.diagramState.diagramList().length === 0);

  ngOnInit(): void {
    // Load the diagram list
    this.loadDiagramList();
  }

  /**
   * Load the list of diagrams
   */
  private loadDiagramList(): void {
    // We're assuming DiagramStateService has a method to load the diagram list
    // This implementation might need to be adjusted based on the actual method
    try {
      // Set loading state
      this.diagramState.setLoading(true);
      
      // Load diagrams from storage
      // In a real implementation, this would likely be asynchronous
      // But since we're working with signals, we don't need to subscribe
      this.diagramState.loadDiagramList().then(() => {
        this.diagramState.setLoading(false);
      }).catch(error => {
        this.logger.error('Failed to load diagram list', 'DiagramHomeComponent', error);
        this.diagramState.setError({
          message: 'Failed to load diagram list',
          details: { error: String(error) }
        });
        this.diagramState.setLoading(false);
      });
    } catch (error) {
      this.logger.error('Error in loadDiagramList', 'DiagramHomeComponent', error);
      this.diagramState.setLoading(false);
    }
  }

  /**
   * Create a new diagram
   */
  createNewDiagram(): void {
    try {
      // Create a new diagram using the state service
      this.diagramState.createNewDiagram('Untitled Diagram', {
        backgroundColor: '#ffffff',
        gridSize: 20,
        snapToGrid: true
      });
      
      // Navigate to the editor
      this.router.navigate(['/diagrams/editor']);
    } catch (error) {
      this.logger.error('Failed to create new diagram:', 'DiagramHomeComponent', error);
      this.diagramState.setError({
        message: 'Failed to create new diagram',
        details: { error: String(error) }
      });
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
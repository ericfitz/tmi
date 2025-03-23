import { Component, AfterViewInit, ViewChild, ElementRef, OnDestroy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { DiagramService } from '../services/diagram.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import * as mx from '@maxgraph/core';
import { Subscription } from 'rxjs';

@Component({
  selector: 'app-diagram-canvas',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './diagram-canvas.component.html',
  styleUrls: ['./diagram-canvas.component.scss']
})
export class DiagramCanvasComponent implements AfterViewInit, OnDestroy {
  @ViewChild('diagramContainer', { static: true }) diagramContainer!: ElementRef;
  
  private graph: mx.Graph | null = null;
  private resizeObserver: ResizeObserver | null = null;
  private subscriptions: Subscription[] = [];

  constructor(
    private diagramService: DiagramService,
    private logger: LoggerService
  ) {}

  ngAfterViewInit(): void {
    this.initializeDiagram();
    this.setupResizeObserver();
  }

  ngOnDestroy(): void {
    // Clean up subscriptions
    this.subscriptions.forEach(sub => sub.unsubscribe());
    
    // Clean up resize observer
    if (this.resizeObserver) {
      this.resizeObserver.disconnect();
    }
  }

  /**
   * Initialize the diagram canvas
   */
  private initializeDiagram(): void {
    try {
      // Get the container element
      const container = this.diagramContainer.nativeElement;
      
      // Initialize the graph
      this.graph = this.diagramService.initGraph(container);
      
      this.logger.info('Diagram canvas initialized', 'DiagramCanvasComponent');
    } catch (error) {
      this.logger.error('Failed to initialize diagram canvas', 'DiagramCanvasComponent', error);
    }
  }

  /**
   * Set up observer to handle container resizing
   */
  private setupResizeObserver(): void {
    // Create a resize observer to handle container size changes
    this.resizeObserver = new ResizeObserver(() => {
      if (this.graph) {
        this.graph.sizeDidChange();
      }
    });
    
    // Observe the container
    this.resizeObserver.observe(this.diagramContainer.nativeElement);
  }
}

import { Component, OnInit, OnDestroy, ChangeDetectionStrategy, inject } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { Subscription } from 'rxjs';
import { DiagramCanvasComponent } from './diagram-canvas/diagram-canvas.component';
import { DiagramToolbarComponent } from './diagram-toolbar/diagram-toolbar.component';
import { NgIf, CommonModule } from '@angular/common';
import { DiagramStateService } from './services/diagram-state.service';
import { LoggerService } from '../shared/services/logger/logger.service';
import { DiagramService } from './services/diagram.service';

@Component({
  selector: 'app-diagram',
  standalone: true,
  imports: [DiagramCanvasComponent, DiagramToolbarComponent, NgIf, CommonModule],
  templateUrl: './diagram.component.html',
  styleUrl: './diagram.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class DiagramComponent implements OnInit, OnDestroy {
  // Inject services
  public diagramState = inject(DiagramStateService);
  private diagramService = inject(DiagramService);
  private route = inject(ActivatedRoute);
  private router = inject(Router);
  private logger = inject(LoggerService);
  
  private subscriptions: Subscription[] = [];

  ngOnInit(): void {
    // Check for diagram ID in route params
    this.subscriptions.push(
      this.route.params.subscribe(async params => {
        const diagramId = params['id'];
        if (diagramId) {
          // Load specific diagram
          await this.loadDiagram(diagramId);
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
    this.diagramState.clearDiagram();
  }

  /**
   * Load a diagram by ID
   */
  private async loadDiagram(diagramId: string): Promise<void> {
    try {
      this.diagramState.setLoading(true);
      
      // Use diagram service to get the diagram data
      await this.diagramService.loadDiagram(diagramId);
      const diagramData = this.diagramService.getCurrentDiagram();
      
      if (!diagramData) {
        throw new Error('Failed to load diagram, data not found');
      }
      
      // Convert diagram data to the format expected by the state service
      const diagram = this.convertToDiagramModel(diagramData);
      
      // Update the state service
      this.diagramState.setCurrentDiagram(diagram);
      this.diagramState.setLoading(false);
    } catch (error) {
      this.logger.error(`Error loading diagram: ${diagramId}`, 'DiagramComponent', error);
      this.diagramState.setError({
        message: error instanceof Error ? error.message : 'Failed to load diagram',
        details: { diagramId }
      });
      this.diagramState.setLoading(false);
      
      // Create a new diagram as a fallback
      this.createNewDiagram();
    }
  }

  /**
   * Create a new diagram if no ID is provided
   */
  private async createNewDiagram(): Promise<void> {
    try {
      // Check if we already have a diagram loaded
      const diagram = this.diagramState.currentDiagram();
      
      if (!diagram) {
        this.diagramState.createNewDiagram('Untitled Diagram', {
          backgroundColor: '#ffffff',
          gridSize: 20,
          snapToGrid: true
        });
      }
    } catch (error) {
      this.logger.error('Error creating new diagram:', 'DiagramComponent', error);
    }
  }

  /**
   * Save the current diagram
   */
  saveDiagram(): void {
    // Get the current diagram from state
    const diagram = this.diagramState.currentDiagram();
    if (!diagram) return;
    
    try {
      this.diagramState.setLoading(true);
      
      // Convert to diagram service format
      const diagramData = {
        id: diagram.id,
        title: diagram.name,
        cells: this.convertElementsToCells(diagram.elements),
        createdAt: diagram.createdAt,
        updatedAt: new Date().toISOString(),
        version: diagram.version.toString(),
        properties: diagram.properties
      };
      
      // First update the diagram data in the service
      // This is a workaround since we need to update the data before saving
      // In a full refactor, we would change the API to take the diagram data directly
      Object.assign(this.diagramService.getCurrentDiagram() || {}, diagramData);
      
      // Now save the diagram using the name as the filename
      this.diagramService.saveDiagram(diagram.name);
      
      // Update state
      this.diagramState.hasChanges.set(false);
      this.diagramState.setLoading(false);
    } catch (error) {
      this.logger.error('Error saving diagram', 'DiagramComponent', error);
      this.diagramState.setError({
        message: 'Failed to save diagram',
        details: { error: String(error) }
      });
      this.diagramState.setLoading(false);
    }
  }

  
  /**
   * Close the current diagram
   */
  closeDiagram(): void {
    // Clear the current diagram
    this.diagramState.clearDiagram();
    
    // Reload the current route to show the welcome message
    this.router.navigateByUrl('/diagrams');
  }

  /**
   * Helper method to convert diagram service model to state model
   */
  private convertToDiagramModel(diagramData: any): any {
    if (!diagramData) {
      this.logger.warn('Attempting to convert null diagram data', 'DiagramComponent');
      return null;
    }

    try {
      // Map diagram data to state model
      const diagram = {
        id: diagramData.id || this.generateUuid(),
        name: diagramData.title || 'Untitled Diagram',
        elements: this.mapCellsToElements(diagramData.cells || []),
        createdAt: diagramData.createdAt || new Date().toISOString(),
        updatedAt: diagramData.updatedAt || new Date().toISOString(),
        version: diagramData.version ? Number(diagramData.version) : 1,
        properties: {
          backgroundColor: '#ffffff',
          gridSize: 20,
          snapToGrid: true,
          ...(diagramData.properties || {})
        }
      };
      
      return diagram;
    } catch (error) {
      this.logger.error('Error converting diagram model', 'DiagramComponent', error);
      
      // Return a basic diagram in case of error
      return {
        id: this.generateUuid(),
        name: 'Untitled Diagram',
        elements: [],
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        version: 1,
        properties: {
          backgroundColor: '#ffffff',
          gridSize: 20,
          snapToGrid: true
        }
      };
    }
  }

  /**
   * Helper method to map DiagramService cells to diagram elements
   */
  private mapCellsToElements(cells: any[]): any[] {
    const elements: any[] = [];
    
    cells.forEach(cell => {
      if (cell.isVertex) {
        elements.push({
          id: cell.id,
          type: this.mapCellStyleToType(cell.style || ''),
          position: {
            x: cell.geometry.x,
            y: cell.geometry.y
          },
          size: {
            width: cell.geometry.width,
            height: cell.geometry.height
          },
          properties: {
            text: cell.value,
            ...this.parseStyleProperties(cell.style || '')
          },
          zIndex: 0 // Default z-index
        });
      } else if (cell.isEdge) {
        elements.push({
          id: cell.id,
          type: 'connector',
          position: { x: 0, y: 0 }, // Edges don't have a position
          size: { width: 0, height: 0 }, // Edges don't have a size
          properties: {
            text: cell.value,
            sourceElementId: cell.sourceId || undefined,
            targetElementId: cell.targetId || undefined,
            ...this.parseStyleProperties(cell.style || '')
          },
          zIndex: 0 // Default z-index
        });
      }
    });
    
    return elements;
  }
  
  /**
   * Helper method to convert elements to cells for diagram service
   */
  private convertElementsToCells(elements: any[]): any[] {
    return elements.map(element => {
      if (element.type === 'connector') {
        return {
          id: element.id,
          isEdge: true,
          isVertex: false,
          value: element.properties.text || '',
          style: this.generateStyleString(element.properties),
          sourceId: element.properties.sourceElementId,
          targetId: element.properties.targetElementId,
          geometry: {
            points: element.properties.points || []
          }
        };
      } else {
        return {
          id: element.id,
          isVertex: true,
          isEdge: false,
          value: element.properties.text || '',
          style: this.generateStyleString(element.properties, element.type),
          geometry: {
            x: element.position.x,
            y: element.position.y,
            width: element.size.width,
            height: element.size.height
          }
        };
      }
    });
  }

  /**
   * Generate a style string from element properties
   */
  private generateStyleString(properties: any, type?: string): string {
    const styles: string[] = [];
    
    if (type) {
      styles.push(`shape=${type}`);
    }
    
    if (properties.backgroundColor) {
      styles.push(`fillColor=${properties.backgroundColor}`);
    }
    
    if (properties.borderColor) {
      styles.push(`strokeColor=${properties.borderColor}`);
    }
    
    if (properties.borderWidth) {
      styles.push(`strokeWidth=${properties.borderWidth}`);
    }
    
    if (properties.color) {
      styles.push(`fontColor=${properties.color}`);
    }
    
    if (properties.fontSize) {
      styles.push(`fontSize=${properties.fontSize}`);
    }
    
    if (properties.fontFamily) {
      styles.push(`fontFamily=${properties.fontFamily}`);
    }
    
    if (properties.opacity) {
      styles.push(`opacity=${properties.opacity}`);
    }
    
    if (properties.imageUrl) {
      styles.push(`imageUrl=${properties.imageUrl}`);
    }
    
    return styles.join(';');
  }

  /**
   * Map cell style to element type
   */
  private mapCellStyleToType(style: string): string {
    if (!style) return 'rectangle';
    
    if (style.includes('shape=circle') || style.includes('shape=ellipse')) {
      return 'circle';
    }
    
    if (style.includes('shape=triangle')) return 'triangle';
    if (style.includes('shape=image')) return 'image';
    if (style.includes('shape=text')) return 'text';
    if (style.includes('shape=line')) return 'line';
    
    return 'rectangle'; // Default
  }

  /**
   * Parse style string into properties object
   */
  private parseStyleProperties(style: string): any {
    if (!style) return {};
    
    const properties: any = {};
    const styleParts = style.split(';');
    
    styleParts.forEach(part => {
      const [key, value] = part.split('=');
      if (key && value) {
        switch (key.trim()) {
          case 'fillColor':
            properties.backgroundColor = value.trim();
            break;
          case 'strokeColor':
            properties.borderColor = value.trim();
            break;
          case 'strokeWidth':
            properties.borderWidth = parseInt(value.trim(), 10);
            break;
          case 'fontColor':
            properties.color = value.trim();
            break;
          case 'fontSize':
            properties.fontSize = parseInt(value.trim(), 10);
            break;
          case 'fontFamily':
            properties.fontFamily = value.trim();
            break;
          case 'opacity':
            properties.opacity = parseFloat(value.trim());
            break;
          case 'imageUrl':
            properties.imageUrl = value.trim();
            break;
        }
      }
    });
    
    return properties;
  }
  
  /**
   * Generate a UUID
   */
  private generateUuid(): string {
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
      const r = Math.random() * 16 | 0;
      const v = c === 'x' ? r : (r & 0x3 | 0x8);
      return v.toString(16);
    });
  }
}
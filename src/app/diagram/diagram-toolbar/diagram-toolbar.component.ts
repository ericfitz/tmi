import { 
  Component, 
  OnInit, 
  Input, 
  Output, 
  EventEmitter, 
  ChangeDetectionStrategy,
  Inject,
  LOCALE_ID
} from '@angular/core';
import { Router } from '@angular/router';
import { CommonModule } from '@angular/common';
import { DiagramService } from '../services/diagram.service';
import { StorageService } from '../../shared/services/storage/storage.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { PickerOptions, PickerResult } from '../../shared/services/storage/providers/storage-provider.interface';
import { DiagramStateService } from '../services/diagram-state.service';
import { DiagramElementType, DiagramProperties, Position, Size, DiagramElementProperties } from '../models/diagram.model';
import { TranslatePipe } from '../../shared/pipes/translate.pipe';
import { FontAwesomeModule } from '@fortawesome/angular-fontawesome';

// Import regular icons
import { 
  faFile, 
  faFolderOpen, 
  faFloppyDisk, 
  faCopy, 
  faPaste
} from '@fortawesome/free-regular-svg-icons';

// Import solid icons
import {
  faFileExport,
  faXmark,
  faSquare,
  faTrash,
  faTable
} from '@fortawesome/free-solid-svg-icons';

@Component({
  selector: 'app-diagram-toolbar',
  standalone: true,
  imports: [CommonModule, TranslatePipe, FontAwesomeModule],
  templateUrl: './diagram-toolbar.component.html',
  styleUrls: ['./diagram-toolbar.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class DiagramToolbarComponent implements OnInit {
  @Input() diagramName = 'Untitled Diagram';
  @Input() isDiagramLoaded = false;
  
  @Output() save = new EventEmitter<void>();
  @Output() close = new EventEmitter<void>();
  
  // Add icon references
  faFile = faFile;
  faFolderOpen = faFolderOpen;
  faFloppyDisk = faFloppyDisk;
  faFileExport = faFileExport;
  faXmark = faXmark;
  faCopy = faCopy;
  faPaste = faPaste;
  faSquare = faSquare;
  faTrash = faTrash;
  faTable = faTable;

  constructor(
    public diagramState: DiagramStateService, // Public to access signals in template
    private diagramService: DiagramService,
    private storageService: StorageService,
    private logger: LoggerService,
    private router: Router,
    @Inject(LOCALE_ID) private locale: string
  ) {}
  
  ngOnInit(): void {
    // Initialize component resources
    this.loadUserPreferences();
  }
  
  private loadUserPreferences(): void {
    // This method will be implemented to load user preferences
    this.logger.debug('Loading user preferences for diagram toolbar', 'DiagramToolbarComponent');
  }

  /**
   * Close the current diagram
   */
  closeDiagram(): void {
    try {
      // Check for unsaved changes
      if (this.diagramState.hasChanges()) {
        const message = 'There are unsaved changes. Do you want to save your changes before closing this diagram?';
        
        if (confirm(message)) {
          this.save.emit(); // Emit the save event so parent can handle it
        }
      }
      
      // Emit close event for parent to handle
      this.close.emit();
    } catch (error) {
      this.logger.error('Failed to close diagram', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Add a new process node to the diagram
   */
  addProcess(): void {
    try {
      const position = { x: Math.random() * 400, y: Math.random() * 400 } as Position;
      const size = { width: 120, height: 60 } as Size;
      const properties = {
        text: 'Process',
        backgroundColor: '#ffffff',
        borderColor: '#000000',
        elementType: 'process'
      };
      
      this.diagramState.addElement(DiagramElementType.PROCESS, position, size, properties);
    } catch (error) {
      this.logger.error('Failed to add process', 'DiagramToolbarComponent', error);
    }
  }


  /**
   * Delete selected elements
   */
  deleteSelected(): void {
    try {
      this.logger.info('Delete button clicked', 'DiagramToolbarComponent');
      
      // Get the selected element ID
      const selectedId = this.diagramState.selectedElementId();
      
      // Check for selection in DiagramService first (for backward compatibility)
      const diagramData = this.diagramService.getCurrentDiagram();
      const selectedCellId = diagramData?.selectedCellId;
      
      if (selectedId || selectedCellId) {
        // Use whichever selection we have
        const idToDelete = selectedId || selectedCellId;
        this.logger.info(`Deleting element with ID: ${idToDelete}`, 'DiagramToolbarComponent');
        
        // Delete from DiagramStateService if it has the element
        if (selectedId) {
          this.diagramState.removeElement(selectedId);
        }
        
        // Make sure DiagramService selection is updated
        if (diagramData && diagramData.selectedCellId) {
          // Call the diagram service's delete method to handle the graph deletion
          this.diagramService.deleteSelected();
        }
      } else {
        // If no element is selected, do nothing - no warning needed
        this.logger.debug('Delete clicked but no element selected', 'DiagramToolbarComponent');
      }
    } catch (error) {
      this.logger.error('Failed to delete selection', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Create a new blank diagram
   */
  async newDiagram(): Promise<void> {
    try {
      // First check if there are unsaved changes
      const hasChanges = this.diagramState.hasChanges();
      
      if (hasChanges) {
        const message = 'There are unsaved changes. Do you want to save your changes before creating a new diagram?';
        
        if (confirm(message)) {
          this.save.emit(); // Emit the save event so parent can handle it
        }
      }
      
      // Create new diagram via state service
      this.diagramState.createNewDiagram('Untitled Diagram', {
        backgroundColor: '#ffffff',
        gridSize: 20,
        snapToGrid: true
      });
      
      // Navigate to the editor page if we're not already there
      if (window.location.pathname !== '/diagrams/editor') {
        // Use the Angular Router to navigate instead of directly manipulating window.location
        // This prevents full page reload which could be causing logout issues
        this.router.navigateByUrl('/diagrams/editor');
      }
    } catch (error) {
      this.logger.error('Failed to create new diagram', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Open diagram from storage
   */
  async openDiagram(): Promise<void> {
    try {
      // Check for unsaved changes
      let shouldProceed = true;
      
      if (this.diagramState.hasChanges()) {
        const message = 'There are unsaved changes. Do you want to save your changes before opening a diagram?';
        
        if (confirm(message)) {
          this.save.emit(); // Emit the save event so parent can handle it
        } else {
          shouldProceed = false;
        }
      }
      
      if (!shouldProceed) return;
      
      // Configure the picker
      const options: PickerOptions = {
        mode: 'open',
        title: 'Open Diagram',
        fileType: ['application/json']
      };
      
      // Show the picker
      const result: PickerResult = await this.storageService.showPicker(options);
      
      // Handle the result
      if (result.action === 'picked' && result.file) {
        // Set loading state
        this.diagramState.setLoading(true);
        
        try {
          // Load the file content
          const content = await this.storageService.loadFile(result.file.id);
          
          // Parse the JSON content
          const diagramData = JSON.parse(content);
          
          // Update the diagram state
          this.diagramState.setCurrentDiagram(diagramData);
          this.diagramState.setCurrentFile(result.file);
          
          // Clear loading state
          this.diagramState.setLoading(false);
        } catch (error) {
          this.logger.error('Failed to load diagram file', 'DiagramToolbarComponent', error);
          this.diagramState.setError({
            message: 'Failed to load diagram',
            details: { error: String(error) }
          });
          this.diagramState.setLoading(false);
        }
      }
    } catch (error) {
      this.logger.error('Failed to open diagram', 'DiagramToolbarComponent', error);
      this.diagramState.setLoading(false);
    }
  }

  /**
   * Toggle grid visibility
   */
  toggleGrid(): void {
    try {
      const currentVisibility = this.diagramState.showGrid();
      const newVisibility = !currentVisibility;
      
      this.logger.info(`Toggling grid visibility from ${currentVisibility} to ${newVisibility}`, 'DiagramToolbarComponent');
      
      // Update in DiagramStateService
      this.diagramState.toggleGrid(newVisibility);
      
      // Also apply the change to the DiagramService to ensure the graph rendering is updated
      if (this.diagramService) {
        // Get the graph from the service if available
        const graph = (this.diagramService as any).graph;
        if (graph && typeof graph.setGridEnabled === 'function') {
          this.logger.debug(`Applying grid visibility ${newVisibility} to graph directly`, 'DiagramToolbarComponent');
          graph.setGridEnabled(newVisibility);
        }
      }
    } catch (error) {
      this.logger.error('Failed to toggle grid', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Handle save event
   */
  onSave(): void {
    this.save.emit();
  }

  /**
   * Backward compatibility getter for the template
   * This allows the template to continue using the async pipe pattern
   * until it's updated to use signals directly
   */
  get hasChanges$() {
    return { 
      async: true, 
      pipe: () => ({ subscribe: (cb: any) => { cb(this.diagramState.hasChanges()); return { unsubscribe: () => {} }; } })
    };
  }
}
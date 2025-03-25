import { 
  Component, 
  OnDestroy, 
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
import { Observable, Subject, firstValueFrom } from 'rxjs';
import { take, takeUntil, map } from 'rxjs/operators';
import { DiagramFacadeService } from '../services/diagram-facade.service';
import { DiagramElementType, DiagramProperties, Position, Size, DiagramElementProperties } from '../store/models/diagram.model';
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
  faUndo,
  faRedo,
  faSquare,
  faCircle,
  faFont,
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
export class DiagramToolbarComponent implements OnInit, OnDestroy {
  @Input() diagramName = 'Untitled Diagram';
  @Input() isDiagramLoaded = false;
  
  @Output() save = new EventEmitter<void>();
  @Output() undo = new EventEmitter<void>();
  @Output() redo = new EventEmitter<void>();
  @Output() close = new EventEmitter<void>();
  
  // Observables for enabling/disabling toolbar buttons
  canUndo$: Observable<boolean>;
  canRedo$: Observable<boolean>;
  hasChanges$: Observable<boolean>;
  
  private destroy$ = new Subject<void>();
  
  // Add icon references
  faFile = faFile;
  faFolderOpen = faFolderOpen;
  faFloppyDisk = faFloppyDisk;
  faFileExport = faFileExport;
  faXmark = faXmark;
  faCopy = faCopy;
  faPaste = faPaste;
  faUndo = faUndo;
  faRedo = faRedo;
  faSquare = faSquare;
  faCircle = faCircle;
  faFont = faFont;
  faTrash = faTrash;
  faTable = faTable;

  constructor(
    private diagramFacade: DiagramFacadeService,
    private diagramService: DiagramService,
    private storageService: StorageService,
    private logger: LoggerService,
    private router: Router,
    @Inject(LOCALE_ID) private locale: string
  ) {
    this.canUndo$ = this.diagramFacade.canUndo$;
    this.canRedo$ = this.diagramFacade.canRedo$;
    this.hasChanges$ = this.diagramFacade.hasChanges$;
  }
  
  ngOnInit(): void {
    // Initialize component resources
    this.loadUserPreferences();
  }
  
  private loadUserPreferences(): void {
    // This method will be implemented to load user preferences
    this.logger.debug('Loading user preferences for diagram toolbar', 'DiagramToolbarComponent');
  }
  
  ngOnDestroy(): void {
    // Clean up subscriptions
    this.destroy$.next();
    this.destroy$.complete();
  }
  
  /**
   * Close the current diagram
   */
  closeDiagram(): void {
    try {
      // Check for unsaved changes
      this.hasChanges$.pipe(take(1)).subscribe(hasChanges => {
        if (hasChanges) {
          const message = 'There are unsaved changes. Do you want to save your changes before closing this diagram?';
          
          if (confirm(message)) {
            this.save.emit(); // Emit the save event so parent can handle it
          }
        }
        
        // Emit close event for parent to handle
        this.close.emit();
      });
    } catch (error) {
      this.logger.error('Failed to close diagram', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Add a new rectangular node to the diagram
   */
  addRectangle(): void {
    try {
      const position = { x: Math.random() * 400, y: Math.random() * 400 } as Position;
      const size = { width: 120, height: 60 } as Size;
      const properties = {
        text: 'Rectangle',
        backgroundColor: '#ffffff',
        borderColor: '#000000'
      };
      
      this.diagramFacade.addElement(DiagramElementType.RECTANGLE, position, size, properties);
    } catch (error) {
      this.logger.error('Failed to add rectangle', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Add a new circular node to the diagram
   */
  addCircle(): void {
    try {
      const position = { x: Math.random() * 400, y: Math.random() * 400 } as Position;
      // For a true circle, width and height should be the same
      const size = { width: 80, height: 80 } as Size;
      const properties = {
        text: 'Circle',
        backgroundColor: '#f0f0f0',
        borderColor: '#000000',
        // Add a specific marker for circle type
        shapeType: 'circle'
      };
      
      this.logger.info('Adding circle element', 'DiagramToolbarComponent');
      this.diagramFacade.addElement(DiagramElementType.CIRCLE, position, size, properties);
    } catch (error) {
      this.logger.error('Failed to add circle', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Add a text element to the diagram
   */
  addText(): void {
    try {
      const position = { x: Math.random() * 400, y: Math.random() * 400 } as Position;
      const size = { width: 150, height: 40 } as Size;
      const properties = {
        text: 'Text Element',
        color: '#000000',
        fontSize: 14,
        fontFamily: 'Arial'
      };
      
      this.diagramFacade.addElement(DiagramElementType.TEXT, position, size, properties);
    } catch (error) {
      this.logger.error('Failed to add text', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Delete selected elements
   */
  deleteSelected(): void {
    try {
      this.logger.info('Delete button clicked - invoking DiagramService deleteSelected', 'DiagramToolbarComponent');
      
      // Call the diagram service's delete method directly
      this.diagramService.deleteSelected();
      
      // Also update the store by clearing selection
      this.diagramFacade.selectElement(null);
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
      let hasChanges = false;
      await firstValueFrom(this.hasChanges$.pipe(take(1)))
        .then(changes => hasChanges = changes)
        .catch(err => this.logger.error('Error checking changes', 'DiagramToolbarComponent', err));
      
      if (hasChanges) {
        const message = 'There are unsaved changes. Do you want to save your changes before creating a new diagram?';
        
        if (confirm(message)) {
          this.save.emit(); // Emit the save event so parent can handle it
        }
      }
      
      // Create new diagram via facade
      await this.diagramFacade.createDiagram('Untitled Diagram', {
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
      
      await this.hasChanges$.pipe(take(1)).forEach(hasChanges => {
        if (hasChanges) {
          const message = 'There are unsaved changes. Do you want to save your changes before opening a diagram?';
          
          if (confirm(message)) {
            this.save.emit(); // Emit the save event so parent can handle it
          } else {
            shouldProceed = false;
          }
        }
      });
      
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
        this.diagramFacade.loadDiagram(result.file.id);
      }
    } catch (error) {
      this.logger.error('Failed to open diagram', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Toggle grid visibility
   */
  toggleGrid(): void {
    this.diagramFacade.toggleGrid(false);
  }

  /**
   * Handle save event
   */
  onSave(): void {
    this.save.emit();
  }

  /**
   * Handle undo event
   */
  onUndo(): void {
    this.undo.emit();
  }

  /**
   * Handle redo event
   */
  onRedo(): void {
    this.redo.emit();
  }
}
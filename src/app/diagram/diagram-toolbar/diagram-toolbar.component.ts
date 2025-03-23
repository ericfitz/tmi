import { Component, OnDestroy, OnInit } from '@angular/core';
import { CommonModule } from '@angular/common';
import { TranslateModule, TranslateService } from '@ngx-translate/core';
import { DiagramService } from '../services/diagram.service';
import { StorageService } from '../../shared/services/storage/storage.service';
import { LoggerService } from '../../shared/services/logger/logger.service';
import { PickerOptions } from '../../shared/services/storage/providers/storage-provider.interface';
import { Subscription } from 'rxjs';

@Component({
  selector: 'app-diagram-toolbar',
  standalone: true,
  imports: [CommonModule, TranslateModule],
  templateUrl: './diagram-toolbar.component.html',
  styleUrls: ['./diagram-toolbar.component.scss']
})
export class DiagramToolbarComponent implements OnInit, OnDestroy {
  // Track diagram dirty state
  isDiagramDirty = false;
  
  // Track current filename
  currentFileName = 'Untitled Diagram';
  
  private subscriptions: Subscription[] = [];

  constructor(
    private diagramService: DiagramService,
    private storageService: StorageService,
    private logger: LoggerService,
    private translate: TranslateService
  ) {}
  
  ngOnInit(): void {
    // Subscribe to dirty state changes
    this.subscriptions.push(
      this.diagramService.isDirty$.subscribe(isDirty => {
        this.isDiagramDirty = isDirty;
      })
    );
    
    // Subscribe to current file changes
    this.subscriptions.push(
      this.diagramService.currentFile$.subscribe(file => {
        if (file) {
          this.currentFileName = file.name;
        } else {
          this.currentFileName = 'Untitled Diagram';
        }
      })
    );
  }
  
  ngOnDestroy(): void {
    // Clean up subscriptions
    this.subscriptions.forEach(sub => sub.unsubscribe());
  }

  /**
   * Add a new node to the diagram
   */
  addNode(): void {
    try {
      // Add a new node at a random position
      const x = Math.random() * 400;
      const y = Math.random() * 400;
      this.diagramService.addNode(x, y, 120, 60, 'New Node');
    } catch (error) {
      this.logger.error('Failed to add node', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Delete selected cells
   */
  deleteSelected(): void {
    try {
      this.diagramService.deleteSelected();
    } catch (error) {
      this.logger.error('Failed to delete selection', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Create a new blank diagram
   */
  newDiagram(): void {
    try {
      // Check if we need to save first
      if (this.isDiagramDirty) {
        const message = this.translate.instant('DIAGRAM.TOOLBAR.UNSAVED_CHANGES', 
          { 0: this.translate.instant('DIAGRAM.TOOLBAR.UNSAVED_CHANGES_CREATE') });
        if (confirm(message)) {
          this.saveDiagram();
        }
      }
      
      // Reset to a blank diagram
      this.diagramService.resetDiagram();
    } catch (error) {
      this.logger.error('Failed to create new diagram', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Open a diagram from storage
   */
  async openDiagram(): Promise<void> {
    try {
      // Check if we need to save first
      if (this.isDiagramDirty) {
        const message = this.translate.instant('DIAGRAM.TOOLBAR.UNSAVED_CHANGES',
          { 0: this.translate.instant('DIAGRAM.TOOLBAR.UNSAVED_CHANGES_OPEN') });
        if (confirm(message)) {
          await this.saveDiagram();
        }
      }
      
      // Configure the picker
      const options: PickerOptions = {
        mode: 'open',
        title: this.translate.instant('DIAGRAM.PICKER.OPEN_TITLE'),
        fileType: ['application/json']
      };
      
      // Show the picker
      const result = await this.storageService.showPicker(options);
      
      // Handle the result
      if (result.action === 'picked' && result.file) {
        await this.diagramService.loadDiagram(result.file.id);
      }
    } catch (error) {
      this.logger.error('Failed to open diagram', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Save the current diagram
   */
  async saveDiagram(): Promise<void> {
    try {
      // Get the current file
      const currentFile = this.diagramService.getCurrentFile();
      
      // If we have a file, save to it
      if (currentFile) {
        await this.diagramService.saveDiagram();
      } 
      // Otherwise, show the save as dialog
      else {
        await this.saveAsDiagram();
      }
    } catch (error) {
      this.logger.error('Failed to save diagram', 'DiagramToolbarComponent', error);
    }
  }

  /**
   * Save the diagram with a new name
   */
  async saveAsDiagram(): Promise<void> {
    try {
      // Configure the picker
      const options: PickerOptions = {
        mode: 'save',
        title: this.translate.instant('DIAGRAM.PICKER.SAVE_TITLE'),
        initialFileName: this.currentFileName,
        fileType: ['application/json']
      };
      
      // Show the picker
      const result = await this.storageService.showPicker(options);
      
      // Handle the result
      if (result.action === 'picked') {
        let fileName = result.fileName;
        
        // If we have a file name from the picker, use it
        if (fileName) {
          // Ensure it has a .json extension
          if (!fileName.toLowerCase().endsWith('.json')) {
            fileName += '.json';
          }
          
          await this.diagramService.saveDiagram(fileName);
        }
        // If we have a file from the picker, use that
        else if (result.file) {
          await this.diagramService.saveDiagram(result.file.name);
        }
      }
    } catch (error) {
      this.logger.error('Failed to save diagram as', 'DiagramToolbarComponent', error);
    }
  }
}

import { Routes } from '@angular/router';
import { DiagramComponent } from './diagram.component';
import { DiagramHomeComponent } from './diagram-home/diagram-home.component';
import { DiagramDeactivateGuard } from '../guards/diagram/diagram-deactivate.guard';

export const diagramRoutes: Routes = [
  { 
    path: '', 
    component: DiagramHomeComponent 
  },
  { 
    path: 'editor', 
    component: DiagramComponent,
    canDeactivate: [DiagramDeactivateGuard]
  },
  { 
    path: 'editor/:id', 
    component: DiagramComponent,
    canDeactivate: [DiagramDeactivateGuard]
  }
];
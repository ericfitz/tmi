import { Routes } from '@angular/router';
import { AuthGuard } from './guards/auth.guard';

export const routes: Routes = [
  { 
    path: '', 
    loadChildren: () => import('./landing/landing-routing.module').then(m => m.LANDING_ROUTES)
  },
  { 
    path: 'diagrams', 
    loadChildren: () => import('./diagram/diagram.routes').then(m => m.DIAGRAM_ROUTES),
    canActivate: [AuthGuard] // Protect the diagrams route
  }
];
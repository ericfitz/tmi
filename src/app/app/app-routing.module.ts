import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';

const routes: Routes = [{ path: 'landing', loadChildren: () => import('../landing/landing.module').then(m => m.LandingModule) }, { path: 'diagrams', loadChildren: () => import('../diagram/diagram.module').then(m => m.DiagramModule) }];

@NgModule({
  imports: [RouterModule.forChild(routes)],
  exports: [RouterModule]
})
export class AppRoutingModule { }

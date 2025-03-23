import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';
import { DiagramComponent } from './diagram.component';
import { DiagramDeactivateGuard } from '../guards/guards/diagram-deactivate.guard';

const routes: Routes = [{ 
  path: '', 
  component: DiagramComponent,
  canDeactivate: [DiagramDeactivateGuard]
}];

@NgModule({
  imports: [RouterModule.forChild(routes)],
  exports: [RouterModule]
})
export class DiagramRoutingModule { }

import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { DiagramRoutingModule } from './diagram-routing.module';
import { DiagramComponent } from './diagram.component';


@NgModule({
  declarations: [
    DiagramComponent
  ],
  imports: [
    CommonModule,
    DiagramRoutingModule
  ]
})
export class DiagramModule { }

import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { TranslateModule } from '@ngx-translate/core';

import { DiagramRoutingModule } from './diagram-routing.module';
import { DiagramComponent } from './diagram.component';
import { DiagramCanvasComponent } from './diagram-canvas/diagram-canvas.component';
import { DiagramToolbarComponent } from './diagram-toolbar/diagram-toolbar.component';

@NgModule({
  declarations: [
    DiagramComponent
  ],
  imports: [
    CommonModule,
    TranslateModule,
    DiagramRoutingModule,
    DiagramCanvasComponent,
    DiagramToolbarComponent
  ]
})
export class DiagramModule { }

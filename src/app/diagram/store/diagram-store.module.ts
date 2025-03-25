import { NgModule } from '@angular/core';
import { StoreModule } from '@ngrx/store';
import { diagramReducer } from './reducers/diagram.reducer';

@NgModule({
  imports: [
    StoreModule.forFeature('diagram', diagramReducer)
  ]
})
export class DiagramStoreModule { }
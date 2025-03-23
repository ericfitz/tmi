import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { AppRoutingModule } from './app-routing.module';

import { AUTH_PROVIDER } from './shared/services/auth/providers/auth-provider.interface';
import { STORAGE_PROVIDER } from './shared/services/storage/providers/storage-provider.interface';
import { AuthFactoryService } from './shared/services/auth/providers/auth-factory.service';
import { StorageFactoryService } from './shared/services/storage/providers/storage-factory.service';

@NgModule({
  declarations: [],
  imports: [
    CommonModule,
    AppRoutingModule
  ],
  providers: [
    { 
      provide: AUTH_PROVIDER, 
      useFactory: (factoryService: AuthFactoryService) => factoryService.createProvider(),
      deps: [AuthFactoryService]
    },
    { 
      provide: STORAGE_PROVIDER, 
      useFactory: (factoryService: StorageFactoryService) => factoryService.createProvider(),
      deps: [StorageFactoryService]
    }
  ]
})
export class AppModule { }

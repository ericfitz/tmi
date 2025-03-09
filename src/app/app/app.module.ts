import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';

import { AppRoutingModule } from './app-routing.module';

import { AUTH_PROVIDER } from './shared/services/auth/providers/auth-provider.interface';
import { STORAGE_PROVIDER } from './shared/services/storage/providers/storage-provider.interface';
import { GoogleAuthProvider } from './shared/services/auth/providers/google-auth.provider';
import { GoogleStorageProvider } from './shared/services/storage/providers/google-storage.provider';

@NgModule({
  declarations: [],
  imports: [
    CommonModule,
    AppRoutingModule
  ],
  providers: [
    { provide: AUTH_PROVIDER, useClass: GoogleAuthProvider },
    { provide: STORAGE_PROVIDER, useClass: GoogleStorageProvider }
  ]
})
export class AppModule { }

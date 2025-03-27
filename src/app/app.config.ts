import { ApplicationConfig, APP_INITIALIZER, provideZoneChangeDetection, importProvidersFrom } from '@angular/core';
import { provideRouter } from '@angular/router';
import { HTTP_INTERCEPTORS, provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';
import { routes } from './app.routes';
import { TranslationService, initializeTranslations } from './shared/services/i18n/translation.service';
import { SecurityService } from './shared/services/security/security.service';
import { AUTH_PROVIDER, AuthProvider } from './shared/services/auth/providers/auth-provider.interface';
import { STORAGE_PROVIDER, StorageProvider } from './shared/services/storage/providers/storage-provider.interface';
import { AuthFactoryService } from './shared/services/auth/providers/auth-factory.service';
import { StorageFactoryService } from './shared/services/storage/providers/storage-factory.service';
import { CsrfInterceptor } from './shared/interceptors/csrf.interceptor';
import { CsrfInitInterceptor } from './shared/interceptors/csrf-init.interceptor';
import { CspHeaderInterceptor } from './shared/interceptors/csp-header.interceptor';
// import { environment } from '../environments/environment';
import { TranslationModuleService } from './shared/services/translation-module.service';
import { registerIcons } from './shared/icons/icon.service';
import { FontAwesomeModule } from '@fortawesome/angular-fontawesome';

// Initialize security features
export function initializeSecurity(securityService: SecurityService): () => Promise<void> {
  return () => securityService.initialize();
}

export const appConfig: ApplicationConfig = {
  providers: [
    importProvidersFrom(TranslationModuleService),
    provideZoneChangeDetection({ 
      eventCoalescing: true,
      // Enable runCoalescing for better performance in rich UIs
      runCoalescing: true 
    }),
    provideRouter(routes),
    
    // Fontawesome icons
    importProvidersFrom(FontAwesomeModule),
    
    // Initialize Font Awesome icons
    {
      provide: APP_INITIALIZER,
      useFactory: () => () => {
        registerIcons();
        return Promise.resolve();
      },
      multi: true
    },
    
    // HTTP Client with CSRF protection
    provideHttpClient(
      withInterceptorsFromDi(),
    ),
    
    // HTTP interceptors for security features
    {
      provide: HTTP_INTERCEPTORS,
      useClass: CsrfInitInterceptor,
      multi: true
    },
    {
      provide: HTTP_INTERCEPTORS,
      useClass: CsrfInterceptor,
      multi: true
    },
    {
      provide: HTTP_INTERCEPTORS,
      useClass: CspHeaderInterceptor,
      multi: true
    },
    
    // App Initialization
    {
      provide: APP_INITIALIZER,
      useFactory: initializeTranslations,
      deps: [TranslationService],
      multi: true
    },
    {
      provide: APP_INITIALIZER,
      useFactory: initializeSecurity,
      deps: [SecurityService],
      multi: true
    },
    
    // Auth provider factory
    { 
      provide: AUTH_PROVIDER, 
      useFactory: (factoryService: AuthFactoryService): AuthProvider => factoryService.createProvider(),
      deps: [AuthFactoryService]
    },
    
    // Storage provider factory
    { 
      provide: STORAGE_PROVIDER, 
      useFactory: (factoryService: StorageFactoryService): StorageProvider => factoryService.createProvider(),
      deps: [StorageFactoryService]
    }
  ]
};
import { Injectable } from '@angular/core';
import { LOCATION_INITIALIZED } from '@angular/common';
import { TranslateService } from '@ngx-translate/core';
import { Inject, LOCALE_ID } from '@angular/core';
import { APP_INITIALIZER } from '@angular/core';
import { Observable, of } from 'rxjs';
import { LoggerService } from '../logger/logger.service';

@Injectable({
  providedIn: 'root'
})
export class TranslationService {
  constructor(
    private translate: TranslateService,
    @Inject(LOCALE_ID) private locale: string,
    private logger: LoggerService
  ) {
    // Set application default language
    this.translate.setDefaultLang('en');
  }

  /**
   * Initialize translations
   */
  initialize(): Promise<void> {
    this.logger.debug('Initializing translations', 'TranslationService');
    const currentLanguage = this.getLanguage();
    
    return new Promise<void>((resolve) => {
      this.translate.use(currentLanguage).subscribe({
        next: () => {
          this.logger.info(`Translations initialized: ${currentLanguage}`, 'TranslationService');
          resolve();
        },
        error: (err) => {
          this.logger.error(`Failed to initialize translations for ${currentLanguage}`, 'TranslationService', err);
          // Fallback to default language
          this.translate.use('en');
          resolve();
        }
      });
    });
  }

  /**
   * Get the current browser language or fallback to default
   */
  private getLanguage(): string {
    this.logger.debug(`Current locale: ${this.locale}`, 'TranslationService');
    
    let language = this.locale.split('-')[0]; // Get language code without region
    
    // Check if we have translations for this language, otherwise use default
    const availableLanguages = ['en', 'es'];
    if (!availableLanguages.includes(language)) {
      this.logger.debug(`Language ${language} not available, using default`, 'TranslationService');
      language = 'en';
    }
    
    return language;
  }

  /**
   * Change the current language
   */
  changeLanguage(lang: string): Observable<any> {
    this.logger.debug(`Changing language to: ${lang}`, 'TranslationService');
    return this.translate.use(lang);
  }

  /**
   * Get a translation for a key
   */
  get(key: string | Array<string>, params?: Object): Observable<string | any> {
    return this.translate.get(key, params);
  }

  /**
   * Get an instant translation for a key
   */
  instant(key: string | Array<string>, params?: Object): string | any {
    return this.translate.instant(key, params);
  }
}

/**
 * Factory function for initializing translations during app bootstrap
 */
export function initializeTranslations(translationService: TranslationService): () => Promise<void> {
  return () => translationService.initialize();
}
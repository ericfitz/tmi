import { Injectable, Inject, signal, computed } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { Observable, of } from 'rxjs';
import { catchError } from 'rxjs/operators';
import { LoggerService } from '../logger/logger.service';
import { TranslateService } from '@ngx-translate/core';
// Allow any string for now during development
// import { TranslationKey } from '../../types/i18n-types';

export type SupportedLanguages = 'en' | 'es' | 'ar' | 'he';
export type LanguageDirection = 'ltr' | 'rtl';

export interface LanguageInfo {
  name: string;
  dir: LanguageDirection;
}

@Injectable({
  providedIn: 'root'
})
export class TranslationService {
  // Available languages with their info
  private availableLanguages: Record<SupportedLanguages, LanguageInfo> = {
    'en': { name: 'English', dir: 'ltr' },
    'es': { name: 'Español', dir: 'ltr' },
    'ar': { name: 'العربية', dir: 'rtl' },
    'he': { name: 'עברית', dir: 'rtl' }
  };
  
  // Currently active language
  private currentLanguage = signal<SupportedLanguages>('en');

  constructor(
    @Inject(DOCUMENT) private document: Document,
    private logger: LoggerService,
    private translateService: TranslateService
  ) {
    // Set up available languages
    this.translateService.addLangs(Object.keys(this.availableLanguages));
    this.translateService.setDefaultLang('en');
  }

  /**
   * Initialize translations
   */
  initialize(): Promise<void> {
    this.logger.debug('Initializing translations', 'TranslationService');
    const currentLanguage = this.getBrowserLanguage();
    
    return new Promise<void>((resolve) => {
      this.setLanguage(currentLanguage)
        .pipe(
          catchError(err => {
            this.logger.error(`Failed to initialize translations for ${currentLanguage}`, 'TranslationService', err);
            // Fallback to default language
            return of('en' as SupportedLanguages);
          })
        )
        .subscribe(() => {
          this.logger.info(`Translations initialized: ${currentLanguage}`, 'TranslationService');
          resolve();
        });
    });
  }
  
  /**
   * Set the language including RTL/LTR direction
   */
  private setLanguage(lang: SupportedLanguages): Observable<SupportedLanguages> {
    // Set text direction based on the language
    const langInfo = this.availableLanguages[lang];
    if (langInfo) {
      this.setTextDirection(langInfo.dir);
    } else {
      this.setTextDirection('ltr');
    }
    
    // Use ngx-translate to set the language
    this.translateService.use(lang);
    this.currentLanguage.set(lang);
    
    return of(lang);
  }
  
  /**
   * Set the document's text direction (RTL or LTR)
   */
  private setTextDirection(direction: LanguageDirection): void {
    this.document.documentElement.dir = direction;
    this.document.documentElement.setAttribute('dir', direction);
    this.document.body.dir = direction;
    
    // Add a CSS class for additional RTL styling if needed
    if (direction === 'rtl') {
      this.document.body.classList.add('rtl-layout');
      this.document.body.classList.remove('ltr-layout');
    } else {
      this.document.body.classList.add('ltr-layout');
      this.document.body.classList.remove('rtl-layout');
    }
    
    this.logger.debug(`Text direction set to: ${direction}`, 'TranslationService');
  }

  /**
   * Get the current browser language or fallback to default
   */
  private getBrowserLanguage(): SupportedLanguages {
    const browserLang = this.translateService.getBrowserLang();
    this.logger.debug(`Browser language detected: ${browserLang}`, 'TranslationService');
    
    // Check if we have translations for this language, otherwise use default
    if (browserLang && Object.keys(this.availableLanguages).includes(browserLang)) {
      return browserLang as SupportedLanguages;
    }
    
    this.logger.debug(`Language ${browserLang} not available, using default`, 'TranslationService');
    return 'en';
  }
  
  /**
   * Get all available languages
   */
  getAvailableLanguages(): { code: SupportedLanguages, name: string, dir: LanguageDirection }[] {
    return Object.entries(this.availableLanguages).map(([code, info]) => ({
      code: code as SupportedLanguages,
      name: info.name,
      dir: info.dir
    }));
  }

  /**
   * Change the current language with lazy loading
   */
  changeLanguage(lang: string): Observable<SupportedLanguages> {
    this.logger.debug(`Changing language to: ${lang}`, 'TranslationService');
    
    // Validate language is supported
    if (!this.availableLanguages[lang as SupportedLanguages]) {
      this.logger.warn(`Language ${lang} is not supported, defaulting to English`, 'TranslationService');
      lang = 'en';
    }
    
    // Use our setLanguage method which handles direction
    return this.setLanguage(lang as SupportedLanguages);
  }

  /**
   * Get the current language
   */
  getCurrentLanguage(): SupportedLanguages {
    return this.currentLanguage();
  }
  
  /**
   * Read-only language signal that components can subscribe to
   */
  readonly language = computed(() => this.currentLanguage());

  /**
   * Get a translation for a key
   * @param key The translation key 
   * @param params Optional parameters for translation interpolation
   */
  get(key: string, params?: Record<string, string | number>): Observable<string> {
    return this.translateService.get(key, params);
  }

  /**
   * Get an instant translation for a key
   * @param key The translation key
   * @param params Optional parameters for translation interpolation
   */
  instant(key: string, params?: Record<string, string | number>): string {
    return this.translateService.instant(key, params);
  }
}

/**
 * Factory function for initializing translations during app bootstrap
 */
export function initializeTranslations(translationService: TranslationService): () => Promise<void> {
  return () => translationService.initialize();
}
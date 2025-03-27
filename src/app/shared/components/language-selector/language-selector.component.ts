import { Component, ChangeDetectionStrategy, signal, effect, OnInit } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { TranslationService } from '../../services/i18n/translation.service';

/**
 * Interface for language configuration
 */
interface Language {
  code: string;
  name: string;
  dir: 'ltr' | 'rtl';
}

@Component({
  selector: 'app-language-selector',
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: './language-selector.component.html',
  styleUrls: ['./language-selector.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class LanguageSelectorComponent implements OnInit {
  currentLang = signal<string>('en');
  languages = signal<Language[]>([]);

  constructor(private translationService: TranslationService) {
    // Setup effect to track language changes
    effect(() => {
      this.currentLang.set(this.translationService.language());
    });
  }

  ngOnInit(): void {
    // Get all available languages
    this.languages.set(this.translationService.getAvailableLanguages());
    
    // Set initial language
    this.currentLang.set(this.translationService.getCurrentLanguage());
  }

  /**
   * Change application language
   */
  changeLanguage(languageCode: string): void {
    if (languageCode && languageCode !== this.currentLang()) {
      this.translationService.changeLanguage(languageCode).subscribe();
      // No need to update currentLang - the effect will handle it
    }
  }
  
  /**
   * Track languages by their code for better performance with ngFor
   */
  trackByLanguageCode(index: number, language: Language): string {
    return language.code;
  }
}
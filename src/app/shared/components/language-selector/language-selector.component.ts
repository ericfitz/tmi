import { Component, OnInit, ChangeDetectionStrategy } from '@angular/core';
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
  currentLang = 'en';
  languages: Language[] = [];

  constructor(private translationService: TranslationService) {}

  ngOnInit(): void {
    // Get all available languages
    this.languages = this.translationService.getAvailableLanguages();
    
    // Get current language
    this.currentLang = this.translationService.getCurrentLanguage();
  }

  /**
   * Change application language
   */
  changeLanguage(languageCode: string): void {
    if (languageCode && languageCode !== this.currentLang) {
      this.translationService.changeLanguage(languageCode).subscribe(() => {
        this.currentLang = languageCode;
      });
    }
  }
  
  /**
   * Track languages by their code for better performance with ngFor
   */
  trackByLanguageCode(index: number, language: Language): string {
    return language.code;
  }
}
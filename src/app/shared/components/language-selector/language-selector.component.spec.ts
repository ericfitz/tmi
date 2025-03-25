import { ComponentFixture, TestBed } from '@angular/core/testing';
import { LanguageSelectorComponent } from './language-selector.component';
import { TranslationService } from '../../services/i18n/translation.service';
import { FormsModule } from '@angular/forms';
import { of } from 'rxjs';

describe('LanguageSelectorComponent', () => {
  let component: LanguageSelectorComponent;
  let fixture: ComponentFixture<LanguageSelectorComponent>;
  let translationServiceSpy: jasmine.SpyObj<TranslationService>;

  beforeEach(async () => {
    translationServiceSpy = jasmine.createSpyObj('TranslationService', [
      'getAvailableLanguages',
      'getCurrentLanguage',
      'changeLanguage'
    ]);
    
    // Mock return values
    translationServiceSpy.getAvailableLanguages.and.returnValue([
      { code: 'en', name: 'English', dir: 'ltr' },
      { code: 'es', name: 'Español', dir: 'ltr' },
      { code: 'ar', name: 'العربية', dir: 'rtl' }
    ]);
    translationServiceSpy.getCurrentLanguage.and.returnValue('en');
    translationServiceSpy.changeLanguage.and.returnValue(of('en' as const));

    await TestBed.configureTestingModule({
      imports: [FormsModule, LanguageSelectorComponent],
      providers: [
        { provide: TranslationService, useValue: translationServiceSpy }
      ]
    }).compileComponents();

    fixture = TestBed.createComponent(LanguageSelectorComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('should initialize with languages and current language', () => {
    expect(component.languages.length).toBe(3);
    expect(component.currentLang).toBe('en');
    expect(translationServiceSpy.getAvailableLanguages).toHaveBeenCalled();
    expect(translationServiceSpy.getCurrentLanguage).toHaveBeenCalled();
  });

  it('should change language when a new language is selected', () => {
    // Setup
    const newLanguage = 'es';
    
    // Execute
    component.changeLanguage(newLanguage);
    
    // Verify
    expect(translationServiceSpy.changeLanguage).toHaveBeenCalledWith(newLanguage);
    expect(component.currentLang).toBe('es');
  });

  it('should not change language when same language is selected', () => {
    // Setup - currentLang is already 'en'
    
    // Execute
    component.changeLanguage('en');
    
    // Verify
    expect(translationServiceSpy.changeLanguage).not.toHaveBeenCalled();
    expect(component.currentLang).toBe('en');
  });

  it('should not change language when empty language is selected', () => {
    // Execute
    component.changeLanguage('');
    
    // Verify
    expect(translationServiceSpy.changeLanguage).not.toHaveBeenCalled();
    expect(component.currentLang).toBe('en');
  });
  
  it('should track languages by code for optimal rendering performance', () => {
    // Test with language objects that have the same code but different names
    const lang1 = { code: 'fr', name: 'French', dir: 'ltr' as const };
    const lang2 = { code: 'fr', name: 'Français', dir: 'ltr' as const };
    
    // The trackBy function should return the same value for both objects
    expect(component.trackByLanguageCode(0, lang1)).toBe('fr');
    expect(component.trackByLanguageCode(1, lang2)).toBe('fr');
    expect(component.trackByLanguageCode(0, lang1))
      .toEqual(component.trackByLanguageCode(1, lang2));
    
    // Test with different language codes
    const lang3 = { code: 'de', name: 'German', dir: 'ltr' as const };
    
    // Should return different tracking values
    expect(component.trackByLanguageCode(0, lang1))
      .not.toEqual(component.trackByLanguageCode(2, lang3));
  });
});
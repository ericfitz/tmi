import { ComponentFixture, TestBed } from '@angular/core/testing';
import { RouterTestingModule } from '@angular/router/testing';
import { TranslateModule, TranslateLoader, TranslateService } from '@ngx-translate/core';
import { of } from 'rxjs';
import { DebugElement, NO_ERRORS_SCHEMA } from '@angular/core';
import { By } from '@angular/platform-browser';

import { HeaderComponent } from './header.component';
import { TranslationService } from '../services/i18n/translation.service';

// Mock translation loader for testing
class MockTranslateLoader implements TranslateLoader {
  getTranslation(lang: string) {
    return of({
      'APP': {
        'TITLE': 'TMI Test',
        'SUBTITLE': 'Test Subtitle'
      },
      'NAV': {
        'HOME': 'Home Test',
        'DIAGRAMS': 'Diagrams Test',
        'ABOUT': 'About Test',
        'HELP': 'Help Test'
      }
    });
  }
}

// Mock the translation service
class MockTranslationService {
  translate = {
    currentLang: 'en',
    onLangChange: of({ lang: 'en' }),
    get: jasmine.createSpy('get').and.returnValue(of('Translated Text')),
    instant: jasmine.createSpy('instant').and.returnValue('Translated Text')
  };
  
  changeLanguage(lang: string) {
    this.translate.currentLang = lang;
    return of({ lang });
  }
}

describe('HeaderComponent', () => {
  let component: HeaderComponent;
  let fixture: ComponentFixture<HeaderComponent>;
  let translate: TranslateService;
  let translationService: TranslationService;
  let de: DebugElement;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [HeaderComponent],
      imports: [
        RouterTestingModule,
        TranslateModule.forRoot({
          loader: { provide: TranslateLoader, useClass: MockTranslateLoader }
        })
      ],
      providers: [
        { provide: TranslationService, useClass: MockTranslationService }
      ],
      schemas: [NO_ERRORS_SCHEMA] // To handle app-login-button
    })
    .compileComponents();

    fixture = TestBed.createComponent(HeaderComponent);
    component = fixture.componentInstance;
    translate = TestBed.inject(TranslateService);
    translationService = TestBed.inject(TranslationService);
    de = fixture.debugElement;
    
    // Set up translation
    translate.use('en');
    
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('should display app title and navigation items', () => {
    // Get elements
    const logoText = de.query(By.css('.logo-text'));
    const logoSubtitle = de.query(By.css('.logo-subtitle'));
    const navItems = de.queryAll(By.css('.main-nav li a'));
    
    // Verify app title
    expect(logoText).toBeTruthy();
    expect(logoSubtitle).toBeTruthy();
    
    // Verify nav items
    expect(navItems.length).toBe(4); // Home, Diagrams, About, Help
  });

  it('should have a language selector', () => {
    const languageSelector = de.query(By.css('.language-selector select'));
    expect(languageSelector).toBeTruthy();
    
    // Should have language options
    const options = de.queryAll(By.css('.language-selector select option'));
    expect(options.length).toBeGreaterThanOrEqual(2); // At least English and Spanish
  });

  it('should change language when selecting a different language', () => {
    // Spy on the changeLanguage method
    spyOn(component, 'changeLanguage').and.callThrough();
    
    // Get language selector
    const languageSelector = de.query(By.css('.language-selector select'));
    
    // Trigger change event
    const mockEvent = {
      target: { value: 'es' }
    } as unknown as Event;
    component.changeLanguage(mockEvent);
    
    // Verify method was called
    expect(component.changeLanguage).toHaveBeenCalled();
  });

  it('should have correct navigation links', () => {
    const navLinks = de.queryAll(By.css('.main-nav li a'));
    
    // Check home link
    expect(navLinks[0].attributes['routerLink']).toBe('/');
    
    // Check diagrams link
    expect(navLinks[1].attributes['routerLink']).toBe('/diagrams');
    
    // Check about link
    expect(navLinks[2].attributes['routerLink']).toBe('/about');
    
    // Check help link
    expect(navLinks[3].attributes['routerLink']).toBe('/help');
  });
});

import { ComponentFixture, TestBed } from '@angular/core/testing';
import { RouterTestingModule } from '@angular/router/testing';
import { TranslateModule, TranslateLoader, TranslateService } from '@ngx-translate/core';
import { of } from 'rxjs';
import { DebugElement, NO_ERRORS_SCHEMA } from '@angular/core';
import { By } from '@angular/platform-browser';

import { LandingComponent } from './landing.component';
import { SharedModule } from '../shared/shared.module';

// Mock translation loader for testing
class MockTranslateLoader implements TranslateLoader {
  getTranslation(lang: string) {
    return of({
      'LANDING': {
        'HERO': {
          'TITLE': 'Test Title',
          'SUBTITLE': 'Test Subtitle',
          'START_BUTTON': 'Start Test',
          'LEARN_MORE': 'Learn More Test'
        },
        'FEATURES': {
          'TITLE': 'Features Test',
          'INTERACTIVE': {
            'TITLE': 'Interactive Test',
            'DESCRIPTION': 'Interactive description test'
          },
          'SECURITY': {
            'TITLE': 'Security Test',
            'DESCRIPTION': 'Security description test'
          },
          'STORAGE': {
            'TITLE': 'Storage Test',
            'DESCRIPTION': 'Storage description test'
          },
          'COLLABORATION': {
            'TITLE': 'Collaboration Test',
            'DESCRIPTION': 'Collaboration description test'
          }
        }
      }
    });
  }
}

describe('LandingComponent', () => {
  let component: LandingComponent;
  let fixture: ComponentFixture<LandingComponent>;
  let translate: TranslateService;
  let de: DebugElement;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [LandingComponent],
      imports: [
        RouterTestingModule,
        TranslateModule.forRoot({
          loader: { provide: TranslateLoader, useClass: MockTranslateLoader }
        }),
        SharedModule
      ],
      schemas: [NO_ERRORS_SCHEMA] // To ignore unknown elements like app-header, app-footer
    })
    .compileComponents();

    fixture = TestBed.createComponent(LandingComponent);
    component = fixture.componentInstance;
    translate = TestBed.inject(TranslateService);
    de = fixture.debugElement;
    
    // Set up translation
    translate.use('en');
    
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('should display hero section with translated content', () => {
    const heroTitle = de.query(By.css('.hero-content h1'));
    const heroSubtitle = de.query(By.css('.hero-subtitle'));
    const startButton = de.query(By.css('.cta-button.primary'));
    const learnMoreButton = de.query(By.css('.cta-button.secondary'));
    
    fixture.detectChanges();
    
    expect(heroTitle.nativeElement.textContent).toContain('Test Title');
    expect(heroSubtitle.nativeElement.textContent).toContain('Test Subtitle');
    expect(startButton.nativeElement.textContent).toContain('Start Test');
    expect(learnMoreButton.nativeElement.textContent).toContain('Learn More Test');
  });

  it('should display features section with translated content', () => {
    const featuresTitle = de.query(By.css('.features-section h2'));
    const featureCards = de.queryAll(By.css('.feature-card'));
    
    fixture.detectChanges();
    
    expect(featuresTitle.nativeElement.textContent).toContain('Features Test');
    expect(featureCards.length).toBe(4);
    
    // Check first feature
    const firstFeatureTitle = featureCards[0].query(By.css('h3'));
    const firstFeatureDescription = featureCards[0].query(By.css('p'));
    expect(firstFeatureTitle.nativeElement.textContent).toContain('Interactive Test');
    expect(firstFeatureDescription.nativeElement.textContent).toContain('Interactive description test');
  });

  it('should have correct navigation links', () => {
    const startDiagramButton = de.query(By.css('.cta-button.primary'));
    const learnMoreButton = de.query(By.css('.cta-button.secondary'));
    
    expect(startDiagramButton.attributes['routerLink']).toBe('/diagrams');
    expect(learnMoreButton.attributes['routerLink']).toBe('/about');
  });

  it('should respond to language changes', () => {
    // Change the mock translations
    spyOn(translate, 'instant').and.returnValue('Changed Title');
    
    // Force update
    fixture.detectChanges();
    
    const heroTitle = de.query(By.css('.hero-content h1'));
    expect(translate.instant).toHaveBeenCalled();
  });
});

import { ComponentFixture, TestBed } from '@angular/core/testing';
import { RouterTestingModule } from '@angular/router/testing';
import { TranslateModule, TranslateLoader, TranslateService } from '@ngx-translate/core';
import { of } from 'rxjs';
import { DebugElement } from '@angular/core';
import { By } from '@angular/platform-browser';

import { FooterComponent } from './footer.component';

// Mock translation loader for testing
class MockTranslateLoader implements TranslateLoader {
  getTranslation(lang: string) {
    return of({
      'FOOTER': {
        'COPYRIGHT': '© 2025 TMI Test',
        'TERMS': 'Terms Test',
        'PRIVACY': 'Privacy Test',
        'CONTACT': 'Contact Test'
      }
    });
  }
}

describe('FooterComponent', () => {
  let component: FooterComponent;
  let fixture: ComponentFixture<FooterComponent>;
  let translate: TranslateService;
  let de: DebugElement;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [FooterComponent],
      imports: [
        RouterTestingModule,
        TranslateModule.forRoot({
          loader: { provide: TranslateLoader, useClass: MockTranslateLoader }
        })
      ]
    })
    .compileComponents();

    fixture = TestBed.createComponent(FooterComponent);
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

  it('should display copyright information', () => {
    const copyright = de.query(By.css('.copyright'));
    expect(copyright).toBeTruthy();
    expect(copyright.nativeElement.textContent).toContain('TMI Test');
  });

  it('should display navigation links', () => {
    const navItems = de.queryAll(By.css('.footer-nav li a'));
    
    // Should have 3 links: Terms, Privacy, Contact
    expect(navItems.length).toBe(3);
    
    // Check content of links
    expect(navItems[0].nativeElement.textContent).toContain('Terms Test');
    expect(navItems[1].nativeElement.textContent).toContain('Privacy Test');
    expect(navItems[2].nativeElement.textContent).toContain('Contact Test');
  });

  it('should have correct routing links', () => {
    const navLinks = de.queryAll(By.css('.footer-nav li a'));
    
    // Check Terms link
    expect(navLinks[0].attributes['routerLink']).toBe('/terms');
    
    // Check Privacy link
    expect(navLinks[1].attributes['routerLink']).toBe('/privacy');
    
    // Check Contact link
    expect(navLinks[2].attributes['routerLink']).toBe('/contact');
  });

  it('should have social links', () => {
    const socialLinks = de.queryAll(By.css('.social-links a'));
    
    // Should have at least one social link
    expect(socialLinks.length).toBeGreaterThan(0);
    
    // Check for target=_blank for security
    expect(socialLinks[0].attributes['target']).toBe('_blank');
    expect(socialLinks[0].attributes['rel']).toBe('noopener noreferrer');
  });
});

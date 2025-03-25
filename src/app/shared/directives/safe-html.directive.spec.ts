import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { SafeHtmlDirective } from './safe-html.directive';
import { SecurityContext } from '@angular/core';
import { DomSanitizer } from '@angular/platform-browser';

// Mock DomSanitizer for testing
class MockDomSanitizer {
  sanitize(context: SecurityContext, value: string): string {
    // Simple mock sanitization that just removes script tags
    return value ? value.replace(/<script\b[^<]*(?:(?!<\/script>)<[^<]*)*<\/script>/gi, '') : '';
  }
}

// Test component that uses the directive
@Component({
  template: `<div [appSafeHtml]="content"></div>`
})
class TestComponent {
  content = '<p>Safe content</p>';
}

describe('SafeHtmlDirective', () => {
  let fixture: ComponentFixture<TestComponent>;
  let component: TestComponent;
  
  beforeEach(() => {
    TestBed.configureTestingModule({
      declarations: [TestComponent],
      imports: [SafeHtmlDirective],
      providers: [
        { provide: DomSanitizer, useClass: MockDomSanitizer }
      ]
    });
    
    fixture = TestBed.createComponent(TestComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });
  
  it('should render safe HTML content', () => {
    const divElement = fixture.debugElement.query(By.css('div')).nativeElement;
    expect(divElement.innerHTML).toBe('<p>Safe content</p>');
  });
  
  it('should sanitize unsafe HTML content', () => {
    component.content = '<p>Text</p><script>alert("XSS")</script>';
    fixture.detectChanges();
    
    const divElement = fixture.debugElement.query(By.css('div')).nativeElement;
    expect(divElement.innerHTML).toBe('<p>Text</p>');
  });
});
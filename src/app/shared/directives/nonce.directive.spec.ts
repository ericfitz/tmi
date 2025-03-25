import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { NonceDirective } from './nonce.directive';
import { SecurityService } from '../services/security/security.service';

// Mock SecurityService for testing
class MockSecurityService {
  generateNonce(context: string): string {
    return 'test-nonce-123';
  }
}

// Test component that uses the directive
@Component({
  template: `
    <script [appNonce]="'script'">console.log('test');</script>
    <style [appNonce]="'style'">.test { color: red; }</style>
  `
})
class TestComponent {}

describe('NonceDirective', () => {
  let fixture: ComponentFixture<TestComponent>;
  
  beforeEach(() => {
    TestBed.configureTestingModule({
      declarations: [TestComponent],
      imports: [NonceDirective],
      providers: [
        { provide: SecurityService, useClass: MockSecurityService }
      ]
    });
    
    fixture = TestBed.createComponent(TestComponent);
    fixture.detectChanges();
  });
  
  it('should add nonce attribute to script element', () => {
    const scriptElement = fixture.debugElement.query(By.css('script')).nativeElement;
    expect(scriptElement.getAttribute('nonce')).toBe('test-nonce-123');
  });
  
  it('should add nonce attribute to style element', () => {
    const styleElement = fixture.debugElement.query(By.css('style')).nativeElement;
    expect(styleElement.getAttribute('nonce')).toBe('test-nonce-123');
  });
});
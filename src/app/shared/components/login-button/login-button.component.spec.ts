import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { of, BehaviorSubject } from 'rxjs';
import { TranslateModule, TranslateLoader, TranslateService } from '@ngx-translate/core';
import { CommonModule } from '@angular/common';
import { DebugElement } from '@angular/core';
import { By } from '@angular/platform-browser';

import { LoginButtonComponent } from './login-button.component';
import { AuthService } from '../../services/auth/auth.service';
import { LoggerService } from '../../services/logger/logger.service';
import { TranslationService } from '../../services/i18n/translation.service';
import { UserInfo } from '../../services/auth/providers/auth-provider.interface';

// Mock translation loader for testing
class MockTranslateLoader implements TranslateLoader {
  getTranslation(lang: string) {
    return of({
      'AUTH': {
        'LOGIN': 'Login Test',
        'LOGOUT': 'Logout Test',
        'LOGGING_IN': 'Logging in Test',
        'LOGGING_OUT': 'Logging out Test'
      }
    });
  }
}

// Mock AuthService
class MockAuthService {
  private _authState = new BehaviorSubject<boolean>(false);
  private _userInfo = new BehaviorSubject<UserInfo | null>(null);
  
  authState$ = this._authState.asObservable();
  userInfo$ = this._userInfo.asObservable();
  
  isAuthenticated() {
    return this._authState.value;
  }
  
  login() {
    return new Promise<void>((resolve) => {
      this._authState.next(true);
      this._userInfo.next({
        id: 'test-user-id',
        name: 'Test User',
        email: 'test@example.com',
        picture: 'https://example.com/profile.jpg'
      });
      resolve();
    });
  }
  
  logout() {
    return new Promise<void>((resolve) => {
      this._authState.next(false);
      this._userInfo.next(null);
      resolve();
    });
  }
}

// Mock LoggerService
class MockLoggerService {
  info() {}
  error() {}
}

// Mock Router
class MockRouter {
  navigate = jasmine.createSpy('navigate');
}

describe('LoginButtonComponent', () => {
  let component: LoginButtonComponent;
  let fixture: ComponentFixture<LoginButtonComponent>;
  let authService: AuthService;
  let router: Router;
  let logger: LoggerService;
  let translate: TranslateService;
  let de: DebugElement;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [],
      imports: [
        CommonModule,
        LoginButtonComponent,
        TranslateModule.forRoot({
          loader: { provide: TranslateLoader, useClass: MockTranslateLoader }
        })
      ],
      providers: [
        { provide: AuthService, useClass: MockAuthService },
        { provide: LoggerService, useClass: MockLoggerService },
        { provide: Router, useClass: MockRouter },
        { provide: TranslationService, useClass: MockTranslateLoader }
      ]
    })
    .compileComponents();

    fixture = TestBed.createComponent(LoginButtonComponent);
    component = fixture.componentInstance;
    authService = TestBed.inject(AuthService);
    router = TestBed.inject(Router);
    logger = TestBed.inject(LoggerService);
    translate = TestBed.inject(TranslateService);
    de = fixture.debugElement;
    
    // Set up translation
    translate.use('en');
    
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('should display login button when not authenticated', () => {
    // Initially the user is not authenticated in our mock
    const loginButton = de.query(By.css('.login-button'));
    expect(loginButton).toBeTruthy();
    expect(loginButton.nativeElement.textContent.trim()).toContain('Login Test');
  });

  it('should call login method when login button is clicked', async () => {
    // Spy on the login method
    spyOn(component, 'onLogin').and.callThrough();
    
    // Get the login button and click it
    const loginButton = de.query(By.css('.login-button'));
    loginButton.nativeElement.click();
    
    // Verify method was called
    expect(component.onLogin).toHaveBeenCalled();
    
    // Wait for async operation
    await fixture.whenStable();
    
    // After login, router.navigate should be called to redirect to diagrams
    expect(router.navigate).toHaveBeenCalledWith(['/diagrams']);
  });

  it('should display user info and logout button when authenticated', async () => {
    // Login the user
    await component.onLogin();
    fixture.detectChanges();
    
    // Check for user info display
    const userInfo = de.query(By.css('.user-info'));
    expect(userInfo).toBeTruthy();
    
    // Should display the user's name
    const userName = de.query(By.css('.user-name'));
    expect(userName.nativeElement.textContent.trim()).toBe('Test User');
    
    // Should have a logout button
    const logoutButton = de.query(By.css('.logout-button'));
    expect(logoutButton).toBeTruthy();
    expect(logoutButton.nativeElement.textContent.trim()).toContain('Logout Test');
  });

  it('should call logout method when logout button is clicked', async () => {
    // First login the user
    await component.onLogin();
    fixture.detectChanges();
    
    // Spy on the logout method
    spyOn(component, 'onLogout').and.callThrough();
    
    // Get the logout button and click it
    const logoutButton = de.query(By.css('.logout-button'));
    logoutButton.nativeElement.click();
    
    // Verify method was called
    expect(component.onLogout).toHaveBeenCalled();
    
    // Wait for async operation
    await fixture.whenStable();
    
    // After logout, router.navigate should be called to redirect to home
    expect(router.navigate).toHaveBeenCalledWith(['/']);
  });

  it('should disable buttons during loading states', async () => {
    // Set loading state
    component.isLoading = true;
    fixture.detectChanges();
    
    // Check login button is disabled
    const loginButton = de.query(By.css('.login-button'));
    expect(loginButton.attributes['disabled']).toBeDefined();
    
    // Login and check logout button
    component.isLoading = false;
    await component.onLogin();
    component.isLoading = true;
    fixture.detectChanges();
    
    // Check logout button is disabled
    const logoutButton = de.query(By.css('.logout-button'));
    expect(logoutButton.attributes['disabled']).toBeDefined();
  });

  it('should show loading text during login/logout operations', async () => {
    // Set loading state for login
    component.isLoading = true;
    fixture.detectChanges();
    
    // Check login button text
    const loginButton = de.query(By.css('.login-button'));
    expect(loginButton.nativeElement.textContent.trim()).toContain('Logging in Test');
    
    // Login and check logout button text during loading
    component.isLoading = false;
    await component.onLogin();
    component.isLoading = true;
    fixture.detectChanges();
    
    // Check logout button text
    const logoutButton = de.query(By.css('.logout-button'));
    expect(logoutButton.nativeElement.textContent.trim()).toContain('Logging out Test');
  });
});
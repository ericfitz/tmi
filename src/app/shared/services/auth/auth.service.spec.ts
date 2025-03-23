import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';
import { first } from 'rxjs/operators';

import { AuthService } from './auth.service';
import { LoggerService } from '../logger/logger.service';
import { AuthProvider, UserInfo } from './providers/auth-provider.interface';

// Create a mock auth provider
class MockAuthProvider implements AuthProvider {
  private _isAuthenticated = false;
  private _userInfo: UserInfo | null = null;
  
  getUserInfo(): Promise<UserInfo | null> {
    return Promise.resolve(this._userInfo);
  }
  
  isAuthenticated(): boolean {
    return this._isAuthenticated;
  }
  
  login(): Promise<UserInfo> {
    this._isAuthenticated = true;
    this._userInfo = {
      id: 'test-user-id',
      name: 'Test User',
      email: 'test@example.com',
      picture: 'https://example.com/profile.jpg'
    };
    
    return Promise.resolve(this._userInfo);
  }
  
  logout(): Promise<void> {
    this._isAuthenticated = false;
    this._userInfo = null;
    
    return Promise.resolve();
  }
  
  silentSignIn(): Promise<boolean> {
    return Promise.resolve(this._isAuthenticated);
  }
}

// Mock Logger
class MockLoggerService {
  debug() {}
  info() {}
  warn() {}
  error() {}
}

describe('AuthService', () => {
  let service: AuthService;
  let authProvider: MockAuthProvider;
  let logger: LoggerService;

  beforeEach(() => {
    authProvider = new MockAuthProvider();
    
    TestBed.configureTestingModule({
      providers: [
        AuthService,
        { provide: LoggerService, useClass: MockLoggerService }
      ]
    });
    
    service = TestBed.inject(AuthService);
    logger = TestBed.inject(LoggerService);
    
    // Set the mock provider manually
    (service as any).authProvider = authProvider;
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
  
  it('should have initial state as not authenticated', () => {
    expect(service.isAuthenticated()).toBeFalse();
    
    // Check that authState$ emits false
    service.authState$.pipe(first()).subscribe(state => {
      expect(state).toBeFalse();
    });
    
    // Check that userInfo$ emits null
    service.userInfo$.pipe(first()).subscribe(userInfo => {
      expect(userInfo).toBeNull();
    });
  });
  
  it('should authenticate user on login', async () => {
    // Call login method
    await service.login();
    
    // Check that isAuthenticated returns true
    expect(service.isAuthenticated()).toBeTrue();
    
    // Check that authState$ emits true
    service.authState$.pipe(first()).subscribe(state => {
      expect(state).toBeTrue();
    });
    
    // Check user info
    const userInfo = await service.getUserInfo();
    expect(userInfo).not.toBeNull();
    expect(userInfo?.id).toBe('test-user-id');
    expect(userInfo?.name).toBe('Test User');
    
    // Check that userInfo$ emits user info
    service.userInfo$.pipe(first()).subscribe(info => {
      expect(info).not.toBeNull();
      expect(info?.id).toBe('test-user-id');
    });
  });
  
  it('should unauthenticate user on logout', async () => {
    // First login
    await service.login();
    expect(service.isAuthenticated()).toBeTrue();
    
    // Then logout
    await service.logout();
    
    // Check that isAuthenticated returns false
    expect(service.isAuthenticated()).toBeFalse();
    
    // Check that authState$ emits false
    service.authState$.pipe(first()).subscribe(state => {
      expect(state).toBeFalse();
    });
    
    // Check user info is null
    const userInfo = await service.getUserInfo();
    expect(userInfo).toBeNull();
    
    // Check that userInfo$ emits null
    service.userInfo$.pipe(first()).subscribe(info => {
      expect(info).toBeNull();
    });
  });
  
  it('should attempt silent sign-in during initialization', async () => {
    // Spy on silentSignIn method
    spyOn(authProvider, 'silentSignIn').and.returnValue(Promise.resolve(true));
    
    // Call initialize method
    await service.initialize();
    
    // Verify silentSignIn was called
    expect(authProvider.silentSignIn).toHaveBeenCalled();
  });
});

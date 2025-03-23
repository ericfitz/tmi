import { TestBed } from '@angular/core/testing';
import { AnonymousAuthProvider } from './anonymous-auth.provider';
import { LoggerService } from '../../logger/logger.service';

describe('AnonymousAuthProvider', () => {
  let provider: AnonymousAuthProvider;
  let mockLoggerService: any;

  beforeEach(() => {
    // Create a mock for the LoggerService
    mockLoggerService = jasmine.createSpyObj('LoggerService', ['debug', 'info', 'error']);

    // Configure testing module
    TestBed.configureTestingModule({
      providers: [
        AnonymousAuthProvider,
        { provide: LoggerService, useValue: mockLoggerService }
      ]
    });

    // Get instances
    provider = TestBed.inject(AnonymousAuthProvider);

    // Clear localStorage before each test
    localStorage.removeItem('anonymous_auth_user');
  });

  it('should be created', () => {
    expect(provider).toBeTruthy();
  });

  it('should not be authenticated by default', () => {
    expect(provider.isAuthenticated()).toBeFalse();
    expect(provider.getUserInfo()).toBeNull();
  });

  it('should authenticate anonymously when login is called', async () => {
    await provider.login();
    
    expect(provider.isAuthenticated()).toBeTrue();
    
    const userInfo = provider.getUserInfo();
    expect(userInfo).toBeTruthy();
    expect(userInfo?.id).toContain('anon_');
    expect(userInfo?.name).toBe('Anonymous User');
    expect(userInfo?.email).toContain('@example.com');
    expect(userInfo?.picture).toBeTruthy();
  });

  it('should persist authentication state in localStorage', async () => {
    await provider.login();
    
    // Verify user is stored in localStorage
    const storedUser = localStorage.getItem('anonymous_auth_user');
    expect(storedUser).toBeTruthy();
    
    // Parse and verify
    const parsedUser = JSON.parse(storedUser!);
    expect(parsedUser.id).toBe(provider.getUserInfo()?.id);
  });

  it('should restore authentication from localStorage', async () => {
    await provider.login();
    const originalUserId = provider.getUserInfo()?.id;
    
    // Create a new instance
    const newProvider = new AnonymousAuthProvider(mockLoggerService);
    
    // Should be authenticated with same user
    expect(newProvider.isAuthenticated()).toBeTrue();
    expect(newProvider.getUserInfo()?.id).toBe(originalUserId);
  });

  it('should clear authentication when logout is called', async () => {
    await provider.login();
    expect(provider.isAuthenticated()).toBeTrue();
    
    await provider.logout();
    expect(provider.isAuthenticated()).toBeFalse();
    expect(provider.getUserInfo()).toBeNull();
    expect(localStorage.getItem('anonymous_auth_user')).toBeNull();
  });
});
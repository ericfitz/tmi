import { TestBed } from '@angular/core/testing';
import { Router, UrlTree } from '@angular/router';
import { AuthGuard } from './auth.guard';
import { AuthService } from '../shared/services/auth/auth.service';

describe('AuthGuard', () => {
  let guard: AuthGuard;
  let authServiceSpy: jasmine.SpyObj<AuthService>;
  let routerSpy: jasmine.SpyObj<Router>;

  beforeEach(() => {
    // Create spy objects for dependencies
    authServiceSpy = jasmine.createSpyObj('AuthService', ['isAuthenticated']);
    routerSpy = jasmine.createSpyObj('Router', ['createUrlTree']);
    
    // Mock the createUrlTree method to return a mock UrlTree
    const mockUrlTree = {} as UrlTree;
    routerSpy.createUrlTree.and.returnValue(mockUrlTree);

    TestBed.configureTestingModule({
      providers: [
        AuthGuard,
        { provide: AuthService, useValue: authServiceSpy },
        { provide: Router, useValue: routerSpy }
      ]
    });

    guard = TestBed.inject(AuthGuard);
  });

  it('should be created', () => {
    expect(guard).toBeTruthy();
  });
  
  it('should allow navigation when user is authenticated', () => {
    // Configure auth service to return true for isAuthenticated
    authServiceSpy.isAuthenticated.and.returnValue(true);
    
    // Check if canActivate returns true
    const result = guard.canActivate();
    expect(result).toBeTrue();
    
    // Router navigation should not be called
    expect(routerSpy.createUrlTree).not.toHaveBeenCalled();
  });
  
  it('should redirect to root when user is not authenticated', () => {
    // Configure auth service to return false for isAuthenticated
    authServiceSpy.isAuthenticated.and.returnValue(false);
    
    // Check if canActivate returns UrlTree
    const result = guard.canActivate();
    expect(result).not.toBeTrue();
    
    // Router navigation should be called with redirect to root
    expect(routerSpy.createUrlTree).toHaveBeenCalledWith(['/']);
  });
});

import { Injectable } from '@angular/core';
import { AuthProvider, UserInfo } from './auth-provider.interface';
import { LoggerService } from '../../logger/logger.service';

/**
 * Anonymous authentication provider for testing purposes
 * Allows access to authenticated routes without real authentication
 */
@Injectable()
export class AnonymousAuthProvider implements AuthProvider {
  private isLoggedIn = false;
  private readonly storageKey = 'anonymous_auth_user';
  private readonly expiryKey = 'anonymous_auth_expiry';
  private user: UserInfo | null = null;

  constructor(private logger: LoggerService) {
    // Check if we have a stored session
    this.checkStoredSession();
  }

  /**
   * Check if there's a stored anonymous session
   */
  private checkStoredSession(): void {
    try {
      const storedUser = sessionStorage.getItem(this.storageKey);
      const expiryStr = sessionStorage.getItem(this.expiryKey);
      
      if (storedUser && expiryStr) {
        const expiry = parseInt(expiryStr, 10);
        
        // Check if session is expired
        if (Date.now() < expiry) {
          this.user = JSON.parse(storedUser);
          this.isLoggedIn = true;
          this.logger.debug('Restored anonymous session', 'AnonymousAuthProvider');
        } else {
          // Session expired, clean up
          this.logger.debug('Anonymous session expired', 'AnonymousAuthProvider');
          this.cleanupSession();
        }
      }
    } catch (error) {
      this.logger.error('Failed to restore anonymous session', 'AnonymousAuthProvider', error);
      this.cleanupSession();
    }
  }
  
  private cleanupSession(): void {
    sessionStorage.removeItem(this.storageKey);
    sessionStorage.removeItem(this.expiryKey);
    this.isLoggedIn = false;
    this.user = null;
  }

  /**
   * Login anonymously - creates a fake user identity
   */
  async login(): Promise<void> {
    this.logger.info('Anonymous login', 'AnonymousAuthProvider');
    
    // Generate a random user ID
    const userId = `anon_${Math.random().toString(36).substring(2, 10)}`;
    
    // Create a fake user
    this.user = {
      id: userId,
      name: 'Anonymous User',
      email: `${userId}@example.com`,
      picture: 'https://ui-avatars.com/api/?name=Anonymous+User&background=random'
    };
    
    // Calculate expiration (24 hours from now)
    const expiryTime = Date.now() + (24 * 60 * 60 * 1000);
    
    // Store in session storage with expiration
    sessionStorage.setItem(this.storageKey, JSON.stringify(this.user));
    sessionStorage.setItem(this.expiryKey, expiryTime.toString());
    
    this.isLoggedIn = true;
    this.logger.debug('Anonymous user created', 'AnonymousAuthProvider', { userId, expiresAt: new Date(expiryTime).toISOString() });
    
    return Promise.resolve();
  }

  /**
   * Logout - simply clears the anonymous session
   */
  async logout(): Promise<void> {
    this.logger.info('Anonymous logout', 'AnonymousAuthProvider');
    
    sessionStorage.removeItem(this.storageKey);
    sessionStorage.removeItem(this.expiryKey);
    this.isLoggedIn = false;
    this.user = null;
    
    return Promise.resolve();
  }

  /**
   * Check if a user is authenticated
   */
  isAuthenticated(): boolean {
    // First check our internal state
    if (this.isLoggedIn && this.user) {
      // Double-check expiration
      const expiryStr = sessionStorage.getItem(this.expiryKey);
      if (expiryStr) {
        const expiry = parseInt(expiryStr, 10);
        return Date.now() < expiry;
      }
    }
    return false;
  }

  /**
   * Get the current user information
   */
  getUserInfo(): UserInfo | null {
    return this.user;
  }

  /**
   * Silent sign-in for anonymous auth provider
   * Simply checks for existing session
   */
  async silentSignIn(): Promise<boolean> {
    this.checkStoredSession();
    return this.isLoggedIn;
  }
}
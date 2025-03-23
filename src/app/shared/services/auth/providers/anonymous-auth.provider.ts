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
      const storedUser = localStorage.getItem(this.storageKey);
      if (storedUser) {
        this.user = JSON.parse(storedUser);
        this.isLoggedIn = true;
        this.logger.debug('Restored anonymous session', 'AnonymousAuthProvider');
      }
    } catch (error) {
      this.logger.error('Failed to restore anonymous session', 'AnonymousAuthProvider', error);
    }
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
    
    // Store in local storage to persist across refreshes
    localStorage.setItem(this.storageKey, JSON.stringify(this.user));
    
    this.isLoggedIn = true;
    this.logger.debug('Anonymous user created', 'AnonymousAuthProvider', { userId });
    
    return Promise.resolve();
  }

  /**
   * Logout - simply clears the anonymous session
   */
  async logout(): Promise<void> {
    this.logger.info('Anonymous logout', 'AnonymousAuthProvider');
    
    localStorage.removeItem(this.storageKey);
    this.isLoggedIn = false;
    this.user = null;
    
    return Promise.resolve();
  }

  /**
   * Check if a user is authenticated
   */
  isAuthenticated(): boolean {
    return this.isLoggedIn;
  }

  /**
   * Get the current user information
   */
  getUserInfo(): UserInfo | null {
    return this.user;
  }
}
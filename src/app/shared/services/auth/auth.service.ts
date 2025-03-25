import { Injectable, Inject } from '@angular/core';
import { AUTH_PROVIDER, AuthProvider, UserInfo } from './providers/auth-provider.interface';
import { BehaviorSubject, Observable } from 'rxjs';
import { LoggerService } from '../logger/logger.service';

@Injectable({
  providedIn: 'root'
})
export class AuthService {
  private authState = new BehaviorSubject<boolean>(false);
  private userInfo = new BehaviorSubject<UserInfo | null>(null);

  constructor(
    @Inject(AUTH_PROVIDER) private provider: AuthProvider,
    private logger: LoggerService
  ) {
    // Initialize the auth state from local storage
    this.logger.debug('Initializing AuthService', 'AuthService');
    this.checkAuthState();
  }
  
  /**
   * Initialize auth service with silent sign-in
   */
  async initialize(): Promise<boolean> {
    this.logger.debug('Attempting silent sign-in', 'AuthService');
    try {
      const success = await this.provider.silentSignIn();
      if (success) {
        this.checkAuthState();
        this.logger.info('Silent sign-in successful', 'AuthService');
      } else {
        this.logger.info('Silent sign-in failed or user not previously signed in', 'AuthService');
      }
      return success;
    } catch (error) {
      this.logger.error('Error during silent sign-in', 'AuthService', error);
      return false;
    }
  }

  private checkAuthState(): void {
    const isAuthenticated = this.provider.isAuthenticated();
    this.authState.next(isAuthenticated);
    this.logger.debug(`Auth state checked: ${isAuthenticated}`, 'AuthService');
    
    if (isAuthenticated) {
      const user = this.provider.getUserInfo();
      this.userInfo.next(user);
      this.logger.debug('User info retrieved', 'AuthService', { userId: user?.id });
    } else {
      this.userInfo.next(null);
      this.logger.debug('No authenticated user', 'AuthService');
    }
  }

  async login(): Promise<void> {
    try {
      this.logger.info('Login attempt', 'AuthService');
      await this.provider.login();
      this.checkAuthState();
      this.logger.info('Login successful', 'AuthService');
    } catch (error) {
      this.logger.error('Login failed', 'AuthService', error);
      throw error;
    }
  }

  async logout(): Promise<void> {
    try {
      this.logger.info('Logout attempt', 'AuthService');
      await this.provider.logout();
      this.checkAuthState();
      this.logger.info('Logout successful', 'AuthService');
    } catch (error) {
      this.logger.error('Logout failed', 'AuthService', error);
      throw error;
    }
  }

  isAuthenticated(): boolean {
    return this.provider.isAuthenticated();
  }

  getUserInfo(): UserInfo | null {
    return this.provider.getUserInfo();
  }

  // Observable streams
  get authState$(): Observable<boolean> {
    return this.authState.asObservable();
  }

  get userInfo$(): Observable<UserInfo | null> {
    return this.userInfo.asObservable();
  }
}
import { Injectable } from '@angular/core';
import { AuthProvider, UserInfo } from './auth-provider.interface';
import { environment } from '../../../../../environments/environment';
import { LoggerService } from '../../logger/logger.service';

// Define the Google Identity Services API types
interface GoogleIdentityServicesAccount {
  id: {
    initialize: (config: GoogleIdentityServicesConfig) => void;
    prompt: (callback: (notification: GooglePromptNotification) => void) => void;
    renderButton: (element: HTMLElement, options: GoogleButtonOptions) => void;
    disableAutoSelect: () => void;
  };
}

interface GoogleIdentityServicesConfig {
  client_id: string;
  callback: (response: GoogleCredentialResponse) => void;
  auto_select: boolean;
  cancel_on_tap_outside: boolean;
}

interface GoogleCredentialResponse {
  credential: string;
}

interface GooglePromptNotification {
  isDisplayed: () => boolean;
  isNotDisplayed: () => boolean;
  isSkippedMoment: () => boolean;
}

interface GoogleButtonOptions {
  theme: 'outline' | 'filled_blue' | 'filled_black';
  size: 'large' | 'medium' | 'small';
  type: 'standard' | 'icon';
}

interface GoogleJwtPayload {
  sub: string;
  email: string;
  name: string;
  picture: string;
  exp: number;
}

// eslint-disable-next-line no-var
declare var google: {
  accounts: GoogleIdentityServicesAccount;
};

@Injectable()
export class GoogleAuthProvider implements AuthProvider {
  private clientId = environment.googleAuth.clientId;
  private tokenKey = 'google_auth_token';
  private userInfoKey = 'google_user_info';
  private readonly tokenExpiryKey = 'google_token_expiry';
  
  constructor(private logger: LoggerService) {
    // Initialize listeners after Google API loads
    this.logger.debug('Initializing GoogleAuthProvider', 'GoogleAuthProvider');
    window.addEventListener('load', () => this.initializeGoogleAuth());
  }

  private initializeGoogleAuth(): void {
    if (typeof google !== 'undefined') {
      this.logger.debug('Google Identity Services loaded, initializing', 'GoogleAuthProvider');
      google.accounts.id.initialize({
        client_id: this.clientId,
        callback: this.handleCredentialResponse.bind(this),
        auto_select: false,
        cancel_on_tap_outside: true,
      });
      this.logger.info('Google Identity Services initialized', 'GoogleAuthProvider');
    } else {
      this.logger.warn('Google Identity Services not available', 'GoogleAuthProvider');
    }
  }

  private handleCredentialResponse(response: GoogleCredentialResponse): void {
    this.logger.debug('Received credential response from Google', 'GoogleAuthProvider');
    
    try {
      // Parse the JWT token from the credential response
      const responsePayload = this.parseJwt(response.credential);
      
      // Store the token and user info in sessionStorage for better security
      sessionStorage.setItem(this.tokenKey, response.credential);
      
      // Store the expiration time
      const expirationTime = responsePayload.exp * 1000; // Convert to milliseconds
      sessionStorage.setItem(this.tokenExpiryKey, expirationTime.toString());
      
      const userInfo: UserInfo = {
        id: responsePayload.sub,
        email: responsePayload.email,
        name: responsePayload.name,
        picture: responsePayload.picture
      };
      
      sessionStorage.setItem(this.userInfoKey, JSON.stringify(userInfo));
      
      this.logger.info('Successfully processed Google credentials', 'GoogleAuthProvider', {
        userId: userInfo.id,
        email: userInfo.email
      });
    } catch (error) {
      this.logger.error('Failed to process Google credentials', 'GoogleAuthProvider', error);
    }
  }

  private parseJwt(token: string): GoogleJwtPayload {
    const base64Url = token.split('.')[1];
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
    const jsonPayload = decodeURIComponent(
      atob(base64)
        .split('')
        .map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
        .join('')
    );
    return JSON.parse(jsonPayload);
  }

  async login(): Promise<void> {
    return new Promise((resolve, reject) => {
      try {
        if (typeof google === 'undefined') {
          reject(new Error('Google Identity Services not loaded'));
          return;
        }

        // Display the One Tap UI or the sign-in button
        google.accounts.id.prompt((notification: GooglePromptNotification) => {
          if (notification.isNotDisplayed() || notification.isSkippedMoment()) {
            // Try rendering the button instead
            const button = document.createElement('div');
            document.body.appendChild(button);
            
            google.accounts.id.renderButton(button, {
              theme: 'outline',
              size: 'large',
              type: 'standard'
            });
            
            // Click handler for manual resolution
            button.addEventListener('click', () => {
              // Clean up after authentication is complete
              setTimeout(() => {
                if (document.body.contains(button)) {
                  document.body.removeChild(button);
                }
              }, 1000);
            });

            resolve();
          } else {
            resolve();
          }
        });
      } catch (error) {
        reject(error);
      }
    });
  }

  async logout(): Promise<void> {
    return new Promise<void>(resolve => {
      // Clear stored tokens
      sessionStorage.removeItem(this.tokenKey);
      sessionStorage.removeItem(this.userInfoKey);
      sessionStorage.removeItem(this.tokenExpiryKey);
      
      // Google's API doesn't have a true "logout" for One Tap
      // So we just clear our stored tokens and revoke if needed
      if (typeof google !== 'undefined') {
        google.accounts.id.disableAutoSelect();
      }
      
      resolve();
    });
  }

  isAuthenticated(): boolean {
    const token = sessionStorage.getItem(this.tokenKey);
    const expiryTimeStr = sessionStorage.getItem(this.tokenExpiryKey);
    
    if (!token || !expiryTimeStr) {
      return false;
    }
    
    try {
      const expirationTime = parseInt(expiryTimeStr, 10);
      const currentTime = Date.now();
      
      // Add some buffer time (5 minutes) to refresh token before it expires
      const refreshBuffer = 5 * 60 * 1000; // 5 minutes in milliseconds
      
      // If token will expire within the buffer window, try to refresh silently
      if (currentTime + refreshBuffer > expirationTime) {
        this.logger.debug('Token nearing expiration, attempting refresh', 'GoogleAuthProvider');
        // Schedule token refresh
        setTimeout(() => this.refreshToken(), 0);
      }
      
      return currentTime < expirationTime;
    } catch (error) {
      this.logger.error('Failed to validate token', 'GoogleAuthProvider', error);
      return false;
    }
  }

  getUserInfo(): UserInfo | null {
    const userInfoString = sessionStorage.getItem(this.userInfoKey);
    
    if (!userInfoString) {
      return null;
    }
    
    try {
      return JSON.parse(userInfoString) as UserInfo;
    } catch (error) {
      this.logger.error('Failed to parse user info', 'GoogleAuthProvider', error);
      return null;
    }
  }

  /**
   * Attempt to refresh the authentication token
   */
  private async refreshToken(): Promise<boolean> {
    if (typeof google === 'undefined') {
      this.logger.warn('Google Identity Services not available for token refresh', 'GoogleAuthProvider');
      return false;
    }
    
    try {
      // Prompt for a refresh but with a minimal UI footprint
      return new Promise<boolean>((resolve) => {
        google.accounts.id.prompt((notification: GooglePromptNotification) => {
          if (notification.isDisplayed() || notification.isNotDisplayed()) {
            // Either way, check if our token was refreshed
            setTimeout(() => {
              // Check if we got a fresh token
              const isAuthenticated = this.isAuthenticated();
              resolve(isAuthenticated);
            }, 1000);
          } else {
            resolve(false);
          }
        });
      });
    } catch (error) {
      this.logger.error('Failed to refresh token', 'GoogleAuthProvider', error);
      return false;
    }
  }

  /**
   * Attempt silent sign-in using stored credentials
   */
  async silentSignIn(): Promise<boolean> {
    // First check if we already have a valid token
    if (this.isAuthenticated()) {
      this.logger.debug('Silent sign-in successful with existing token', 'GoogleAuthProvider');
      return true;
    }

    // If not, try refreshing the token
    const refreshed = await this.refreshToken();
    if (refreshed) {
      this.logger.debug('Silent token refresh successful', 'GoogleAuthProvider');
      return true;
    }

    return false;
  }
}
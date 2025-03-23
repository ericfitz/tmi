import { Injectable } from '@angular/core';
import { AuthProvider, UserInfo } from './auth-provider.interface';
import { environment } from '../../../../../environments/environment';
import { LoggerService } from '../../logger/logger.service';

declare var google: any;

@Injectable()
export class GoogleAuthProvider implements AuthProvider {
  private clientId = environment.googleAuth.clientId;
  private tokenKey = 'google_auth_token';
  private userInfoKey = 'google_user_info';
  
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

  private handleCredentialResponse(response: any): void {
    this.logger.debug('Received credential response from Google', 'GoogleAuthProvider');
    
    try {
      // Parse the JWT token from the credential response
      const responsePayload = this.parseJwt(response.credential);
      
      // Store the token and user info
      localStorage.setItem(this.tokenKey, response.credential);
      
      const userInfo: UserInfo = {
        id: responsePayload.sub,
        email: responsePayload.email,
        name: responsePayload.name,
        picture: responsePayload.picture
      };
      
      localStorage.setItem(this.userInfoKey, JSON.stringify(userInfo));
      
      this.logger.info('Successfully processed Google credentials', 'GoogleAuthProvider', {
        userId: userInfo.id,
        email: userInfo.email
      });
    } catch (error) {
      this.logger.error('Failed to process Google credentials', 'GoogleAuthProvider', error);
    }
  }

  private parseJwt(token: string): any {
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
        google.accounts.id.prompt((notification: any) => {
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
      localStorage.removeItem(this.tokenKey);
      localStorage.removeItem(this.userInfoKey);
      
      // Google's API doesn't have a true "logout" for One Tap
      // So we just clear our stored tokens and revoke if needed
      if (typeof google !== 'undefined') {
        google.accounts.id.disableAutoSelect();
      }
      
      resolve();
    });
  }

  isAuthenticated(): boolean {
    const token = localStorage.getItem(this.tokenKey);
    
    if (!token) {
      return false;
    }
    
    try {
      // Parse the token and check if it's expired
      const payload = this.parseJwt(token);
      const expirationTime = payload.exp * 1000; // Convert to milliseconds
      
      return Date.now() < expirationTime;
    } catch (e) {
      return false;
    }
  }

  getUserInfo(): UserInfo | null {
    const userInfoString = localStorage.getItem(this.userInfoKey);
    
    if (!userInfoString) {
      return null;
    }
    
    try {
      return JSON.parse(userInfoString) as UserInfo;
    } catch (e) {
      return null;
    }
  }
}
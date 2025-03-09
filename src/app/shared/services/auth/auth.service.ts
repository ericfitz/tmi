import { Injectable, Inject } from '@angular/core';
import { AUTH_PROVIDER, AuthProvider } from './providers/auth-provider.interface';

@Injectable({
  providedIn: 'root'
})
export class AuthService {
  constructor(@Inject(AUTH_PROVIDER) private provider: AuthProvider) {}

  login(): Promise<void> {
    return this.provider.login();
  }

  logout(): Promise<void> {
    return this.provider.logout();
  }

  isAuthenticated(): boolean {
    return this.provider.isAuthenticated();
  }

  getUserInfo(): any {
    return this.provider.getUserInfo();
  }
}
export const AUTH_PROVIDER = 'AuthProvider';

export interface AuthProvider {
  login(): Promise<void>;
  logout(): Promise<void>;
  isAuthenticated(): boolean;
  getUserInfo(): any;
}
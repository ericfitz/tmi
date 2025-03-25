export const AUTH_PROVIDER = 'AuthProvider';

export interface UserInfo {
  id: string;
  email: string;
  name: string;
  picture?: string;
}

export interface AuthProvider {
  login(): Promise<void>;
  logout(): Promise<void>;
  isAuthenticated(): boolean;
  getUserInfo(): UserInfo | null;
  silentSignIn(): Promise<boolean>;
}
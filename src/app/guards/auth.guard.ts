// src/app/guards/auth.guard.ts
import { Injectable } from '@angular/core';
import { CanActivate, Router, UrlTree } from '@angular/router';
import { AuthService } from '../shared/services/auth/auth.service';

@Injectable({
  providedIn: 'root'
})
export class AuthGuard implements CanActivate {
  constructor(private authService: AuthService, private router: Router) {}

  canActivate(): boolean | UrlTree {
    if (this.authService.isAuthenticated()) {
      return true; // Allow navigation
    } else {
      // Redirect to landing page if not authenticated
      return this.router.createUrlTree(['/']);
    }
  }
}
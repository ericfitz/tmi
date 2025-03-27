import { Component, OnInit } from '@angular/core';
import { Router, RouterOutlet, NavigationEnd } from '@angular/router';
import { HeaderComponent } from './shared/header/header.component';
import { FooterComponent } from './shared/footer/footer.component';
import { TranslateModule } from '@ngx-translate/core';
import { AuthService } from './shared/services/auth/auth.service';
import { LoggerService } from './shared/services/logger/logger.service';
import { filter } from 'rxjs/operators';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'app-root',
  standalone: true,
  imports: [RouterOutlet, HeaderComponent, FooterComponent, TranslateModule, CommonModule],
  templateUrl: './app.component.html',
  styleUrl: './app.component.scss'
})
export class AppComponent implements OnInit {
  isHomePage: boolean = true;

  constructor(
    private router: Router,
    private authService: AuthService,
    private logger: LoggerService
  ) {
    this.clearSessions();
  }

  ngOnInit(): void {
    // Monitor route changes
    this.router.events.pipe(
      filter(event => event instanceof NavigationEnd)
    ).subscribe((event: NavigationEnd) => {
      // Update if we're on home page
      this.isHomePage = event.url === '/' || event.url === '';
      
      // Redirect to home if accessing protected routes while not authenticated
      const protectedRoutes = ['/diagrams'];
      if (protectedRoutes.some(route => event.url.startsWith(route)) && !this.authService.isAuthenticated()) {
        this.logger.warn('Attempted to access protected route while not authenticated', 'AppComponent');
        this.router.navigate(['/']);
      }
    });
  }

  /**
   * Clear all sessions on application startup
   * This ensures fresh state for each app launch
   */
  private clearSessions(): void {
    this.logger.info('Clearing all sessions on application startup', 'AppComponent');
    
    // Clear authentication sessions
    sessionStorage.removeItem('anonymous_auth_user');
    sessionStorage.removeItem('anonymous_auth_expiry');
    sessionStorage.removeItem('google_auth_token');
    sessionStorage.removeItem('google_user_info');
    sessionStorage.removeItem('google_token_expiry');
    
    // Clear storage metadata
    localStorage.removeItem('tmi_files_metadata');
    
    // Remove any file-specific data
    const storageKeys = Object.keys(localStorage);
    storageKeys.forEach(key => {
      if (key.startsWith('tmi_files_')) {
        localStorage.removeItem(key);
      }
    });

    // Force logout
    this.authService.logout().catch(err => {
      this.logger.error('Error during logout', 'AppComponent', err);
    });
  }
}
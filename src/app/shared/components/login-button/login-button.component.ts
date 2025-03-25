import { Component, OnDestroy, OnInit } from '@angular/core';
import { CommonModule } from '@angular/common';
import { Router } from '@angular/router';
import { TranslateModule, TranslateService } from '@ngx-translate/core';
import { AuthService } from '../../services/auth/auth.service';
import { UserInfo } from '../../services/auth/providers/auth-provider.interface';
import { LoggerService } from '../../services/logger/logger.service';
import { Subscription } from 'rxjs';
import { FontAwesomeModule } from '@fortawesome/angular-fontawesome';
import { faGoogle } from '@fortawesome/free-brands-svg-icons';
import { faRightToBracket, faRightFromBracket } from '@fortawesome/free-solid-svg-icons';

@Component({
  selector: 'app-login-button',
  standalone: true,
  imports: [CommonModule, TranslateModule, FontAwesomeModule],
  templateUrl: './login-button.component.html',
  styleUrls: ['./login-button.component.scss']
})
export class LoginButtonComponent implements OnInit, OnDestroy {
  isAuthenticated = false;
  userInfo: UserInfo | null = null;
  isLoading = false;
  
  // Icon references
  faLogin = faRightToBracket;
  faLogout = faRightFromBracket;
  faGoogle = faGoogle;
  
  private subscriptions: Subscription[] = [];

  constructor(
    private authService: AuthService,
    private router: Router,
    private logger: LoggerService,
    private translate: TranslateService
  ) {}

  ngOnInit(): void {
    // Subscribe to auth state changes
    this.subscriptions.push(
      this.authService.authState$.subscribe(isAuthenticated => {
        this.isAuthenticated = isAuthenticated;
      })
    );
    
    // Subscribe to user info changes
    this.subscriptions.push(
      this.authService.userInfo$.subscribe(userInfo => {
        this.userInfo = userInfo;
      })
    );
  }

  ngOnDestroy(): void {
    // Clean up subscriptions
    this.subscriptions.forEach(sub => sub.unsubscribe());
  }

  /**
   * Handle login button click
   */
  async onLogin(): Promise<void> {
    if (this.isLoading) {
      return;
    }
    
    this.isLoading = true;
    
    try {
      this.logger.info('Login initiated', 'LoginButtonComponent');
      await this.authService.login();
      
      // Navigate to diagrams page after successful login
      if (this.authService.isAuthenticated()) {
        this.router.navigate(['/diagrams']);
      }
    } catch (error) {
      this.logger.error('Login failed', 'LoginButtonComponent', error);
    } finally {
      this.isLoading = false;
    }
  }

  /**
   * Handle logout button click
   */
  async onLogout(): Promise<void> {
    if (this.isLoading) {
      return;
    }
    
    this.isLoading = true;
    
    try {
      this.logger.info('Logout initiated', 'LoginButtonComponent');
      await this.authService.logout();
      
      // Navigate to home page after logout
      this.router.navigate(['/']);
    } catch (error) {
      this.logger.error('Logout failed', 'LoginButtonComponent', error);
    } finally {
      this.isLoading = false;
    }
  }
}
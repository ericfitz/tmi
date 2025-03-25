import { Component, OnInit, OnDestroy } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';
import { CommonModule } from '@angular/common';
import { LoginButtonComponent } from '../components/login-button/login-button.component';
import { LanguageSelectorComponent } from '../components/language-selector/language-selector.component';
import { TranslateService, TranslateModule } from '@ngx-translate/core';
import { AuthService } from '../services/auth/auth.service';
import { Subject } from 'rxjs';
import { takeUntil } from 'rxjs/operators';

@Component({
  selector: 'app-header',
  standalone: true,
  imports: [
    CommonModule,
    RouterLink,
    RouterLinkActive,
    TranslateModule,
    LoginButtonComponent,
    LanguageSelectorComponent
  ],
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss']
})
export class HeaderComponent implements OnInit, OnDestroy {
  isAuthenticated = false;
  private destroy$ = new Subject<void>();
  
  constructor(
    private translate: TranslateService,
    private authService: AuthService
  ) {}
  
  ngOnInit(): void {
    // Subscribe to auth state changes
    this.authService.authState$
      .pipe(takeUntil(this.destroy$))
      .subscribe(isAuthenticated => {
        this.isAuthenticated = isAuthenticated;
      });
      
    // Initialize with current auth state
    this.isAuthenticated = this.authService.isAuthenticated();
  }
  
  ngOnDestroy(): void {
    // Clean up subscriptions
    this.destroy$.next();
    this.destroy$.complete();
  }
  
  // Add language change handler for language selector
  changeLanguage(event: Event): void {
    const target = event.target as HTMLSelectElement;
    if (target && target.value) {
      this.translate.use(target.value);
    }
  }
}

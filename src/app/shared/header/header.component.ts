import { Component, effect } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';
import { CommonModule } from '@angular/common';
import { LoginButtonComponent } from '../components/login-button/login-button.component';
import { LanguageSelectorComponent } from '../components/language-selector/language-selector.component';
import { TranslateService, TranslateModule } from '@ngx-translate/core';
import { AuthService } from '../services/auth/auth.service';

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
export class HeaderComponent {
  isAuthenticated = false;
  
  constructor(
    private translate: TranslateService,
    private authService: AuthService
  ) {
    // Set initial value
    this.isAuthenticated = this.authService.isAuthenticated();
    
    // Setup effect to react to auth state changes
    effect(() => {
      this.isAuthenticated = this.authService.isAuthenticated();
    });
  }
  
  // Add language change handler for language selector
  changeLanguage(event: Event): void {
    const target = event.target as HTMLSelectElement;
    if (target && target.value) {
      this.translate.use(target.value);
    }
  }
}

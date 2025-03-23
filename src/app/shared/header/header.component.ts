import { Component, OnInit } from '@angular/core';
import { RouterModule } from '@angular/router';
import { LoginButtonComponent } from '../components/login-button/login-button.component';
import { TranslateModule } from '@ngx-translate/core';
import { TranslationService } from '../services/i18n/translation.service';

@Component({
  selector: 'app-header',
  standalone: false,
  templateUrl: './header.component.html',
  styleUrls: ['./header.component.scss']
})
export class HeaderComponent implements OnInit {
  currentLang = 'en';

  constructor(private translationService: TranslationService) {}

  ngOnInit(): void {
    // Get the current language
    this.currentLang = this.translationService.translate.currentLang;
  }

  /**
   * Change the application language
   */
  changeLanguage(event: Event): void {
    const select = event.target as HTMLSelectElement;
    const lang = select.value;
    
    if (lang) {
      this.translationService.changeLanguage(lang);
      this.currentLang = lang;
    }
  }
}

import { Routes } from '@angular/router';
import { LandingComponent } from './landing.component';
import { AboutComponent } from './about/about.component';
import { HelpComponent } from './help/help.component';
import { PrivacyComponent } from './privacy/privacy.component';
import { TermsOfServiceComponent } from './terms-of-service/terms-of-service.component';

export const LANDING_ROUTES: Routes = [
  {
    path: '',
    component: LandingComponent
  },
  {
    path: 'about',
    component: AboutComponent
  },
  {
    path: 'help',
    component: HelpComponent
  },
  {
    path: 'privacy',
    component: PrivacyComponent
  },
  {
    path: 'terms',
    component: TermsOfServiceComponent
  }
];

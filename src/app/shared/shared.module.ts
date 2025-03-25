import { NgModule } from '@angular/core';
import { CommonModule } from '@angular/common';
import { RouterModule } from '@angular/router';
import { HeaderComponent } from './header/header.component';
import { FooterComponent } from './footer/footer.component';
import { FilePickerComponent } from './components/file-picker/file-picker.component';
import { LoginButtonComponent } from './components/login-button/login-button.component';
import { NonceDirective } from './directives/nonce.directive';
import { SafeHtmlDirective } from './directives/safe-html.directive';
import { TranslatePipe } from './pipes/translate.pipe';

@NgModule({
  imports: [
    CommonModule,
    RouterModule,
    HeaderComponent,
    FooterComponent,
    FilePickerComponent,
    LoginButtonComponent,
    NonceDirective,
    SafeHtmlDirective,
    TranslatePipe
  ],
  exports: [
    HeaderComponent,
    FooterComponent,
    FilePickerComponent,
    LoginButtonComponent,
    NonceDirective,
    SafeHtmlDirective,
    TranslatePipe
  ]
})
export class SharedModule { }
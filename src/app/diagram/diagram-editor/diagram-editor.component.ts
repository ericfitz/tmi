import { Component, ChangeDetectionStrategy, OnInit } from '@angular/core';
import { CommonModule } from '@angular/common';
import { SharedModule } from '../../shared/shared.module';
import { SecurityService } from '../../shared/services/security/security.service';

@Component({
  selector: 'app-diagram-editor',
  standalone: true,
  imports: [CommonModule, SharedModule],
  templateUrl: './diagram-editor.component.html',
  styleUrl: './diagram-editor.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class DiagramEditorComponent implements OnInit {
  dynamicHtml = '<p>This HTML content is sanitized</p>';
  scriptNonce = '';
  styleNonce = '';
  
  constructor(private securityService: SecurityService) {}
  
  ngOnInit(): void {
    // Generate nonces for inline content if needed
    this.scriptNonce = this.securityService.generateNonce('script');
    this.styleNonce = this.securityService.generateNonce('style');
    
    // Add custom domain to CSP for third-party services if needed
    if (this.isExternalResourceRequired()) {
      this.securityService.addTrustedDomains('script-src', ['https://analytics.example.com']);
      this.securityService.addTrustedDomains('img-src', ['https://cdn.example.com']);
    }
  }
  
  /**
   * Example of sanitizing a URL from user input
   */
  sanitizeUserProvidedUrl(url: string): string {
    return this.securityService.sanitizeUrl(url);
  }
  
  /**
   * Determine if external resources are needed
   */
  private isExternalResourceRequired(): boolean {
    // Logic to determine if external resources are required
    return false;
  }
}

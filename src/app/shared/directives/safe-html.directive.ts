import { Directive, ElementRef, Input, OnChanges } from '@angular/core';
import { SecurityService } from '../services/security/security.service';

/**
 * Directive for safely binding HTML content
 */
@Directive({
  selector: '[appSafeHtml]',
  standalone: true
})
export class SafeHtmlDirective implements OnChanges {
  @Input() appSafeHtml = '';
  
  constructor(
    private el: ElementRef,
    private securityService: SecurityService
  ) {}
  
  ngOnChanges(): void {
    const sanitizedHtml = this.securityService.sanitizeHtml(this.appSafeHtml);
    this.el.nativeElement.innerHTML = sanitizedHtml;
  }
}
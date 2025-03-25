import { Directive, ElementRef, Input, OnInit } from '@angular/core';
import { SecurityService } from '../services/security/security.service';

/**
 * Directive that applies a nonce attribute to the element
 * Useful for inline scripts and styles that need to comply with CSP
 */
@Directive({
  selector: '[appNonce]',
  standalone: true
})
export class NonceDirective implements OnInit {
  @Input() appNonce = 'script';
  
  constructor(
    private el: ElementRef,
    private securityService: SecurityService
  ) {}
  
  ngOnInit(): void {
    const nonce = this.securityService.generateNonce(this.appNonce);
    if (nonce) {
      this.el.nativeElement.setAttribute('nonce', nonce);
    }
  }
}
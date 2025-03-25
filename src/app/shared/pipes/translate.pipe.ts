import { Pipe, PipeTransform } from '@angular/core';
import { TranslationService } from '../services/i18n/translation.service';
// Allow any string for now during development
// import { TranslationKey } from '../types/i18n-types';

@Pipe({
  name: 'translate',
  standalone: true
})
export class TranslatePipe implements PipeTransform {
  constructor(private translationService: TranslationService) {}

  transform(key: string, params?: Record<string, string | number>): string {
    return this.translationService.instant(key, params);
  }
}
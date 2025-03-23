# TMI Internationalization Guide

This document explains how to manage translations and add new languages to the TMI application.

## Overview

TMI uses the [ngx-translate](https://github.com/ngx-translate/core) library for handling translations. The application is designed to be fully internationalized, with all user-facing strings stored in translation files.

## Project Structure

- Translation files are located in `src/assets/i18n/` directory
- Each language has its own JSON file (e.g., `en.json` for English, `es.json` for Spanish)
- The `TranslationService` handles language switching and provides translation functionality
- Translation keys follow a hierarchical structure like `SECTION.SUBSECTION.KEY`

## How to Add a New Language

1. **Create a new translation file**:
   - Copy an existing file like `en.json` to a new file named with the appropriate language code (e.g., `fr.json` for French)
   - Translate all text values while keeping the keys unchanged

2. **Register the new language**:
   - Open `src/app/shared/services/i18n/translation.service.ts`
   - Add the new language code to the `availableLanguages` array:
     ```typescript
     const availableLanguages = ['en', 'es', 'fr'];
     ```

3. **Add language to the language selector**:
   - Open `src/app/shared/header/header.component.html`
   - Add a new option to the language selector dropdown:
     ```html
     <option value="fr" [selected]="currentLang === 'fr'">Français</option>
     ```

4. **Test the new language**:
   - Run the application and test the language switcher
   - Verify all UI elements are properly translated
   - Make sure no translation keys are missing

## Translation File Structure

The translation files follow a hierarchical JSON structure to organize translations by feature area:

```json
{
  "APP": {
    "TITLE": "TMI",
    "SUBTITLE": "Threat Modeling Improved"
  },
  "NAV": {
    "HOME": "Home",
    "DIAGRAMS": "Diagrams",
    "ABOUT": "About"
  },
  // ... other sections
}
```

## Best Practices

1. **Always use translation keys**:
   - Never hardcode user-visible strings in templates or components
   - Use the translate pipe in templates: `{{ 'KEY.NAME' | translate }}`
   - For dynamic strings, use the translation service: `this.translate.instant('KEY.NAME')`

2. **Use parameters for dynamic content**:
   - For strings with variable content, use parameters:
     ```html
     {{ 'GREETING' | translate:{ name: userName } }}
     ```
     With a translation like: `"GREETING": "Hello, {name}!"`

3. **Keep keys organized**:
   - Use a consistent naming convention for keys
   - Group related keys under meaningful parent keys
   - Document complex keys or those with parameters

4. **Handle pluralization**:
   - Use ngx-translate's pluralization features for content that needs to change based on count
   - See ngx-translate documentation for details on pluralization syntax

5. **Test thoroughly**:
   - Always test the application in all supported languages after adding new features
   - Pay special attention to text that might expand in other languages and break layouts

## Troubleshooting

- **Missing translations**: If a translation key is not found, ngx-translate will display the key itself
- **Language not loading**: Check that the language file exists and has the correct format
- **Text doesn't fit UI elements**: Consider using flexible layouts and avoid fixed-width containers

## Adding Translation Keys

When adding new features to the application that require user-visible text:

1. Add the appropriate keys to ALL language files
2. Follow the existing key structure and naming conventions
3. Provide accurate translations for all supported languages
4. Use descriptive keys that indicate the purpose of the text

## Resources

- [ngx-translate Documentation](https://github.com/ngx-translate/core)
- [Angular i18n Guide](https://angular.io/guide/i18n-overview)
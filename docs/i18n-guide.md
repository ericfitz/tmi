# TMI Internationalization Guide

This document explains how to manage translations and add new languages to the TMI application.

## Overview

TMI uses the ngx-translate library for handling translations. The application is designed to be fully internationalized, with all user-facing strings stored in translation files.

## Key Features

- **Lazy-loaded translations**: Translation files are loaded on-demand to improve performance
- **RTL (Right-to-Left) support**: Full support for RTL languages like Arabic and Hebrew
- **Language detection**: Automatic language detection based on browser settings
- **Dynamic language switching**: Users can change languages without refreshing the app

## Project Structure

- Translation files are located in `src/assets/i18n/` directory
- Each language has its own JSON file (e.g., `en.json` for English, `es.json` for Spanish)
- The `TranslationService` handles language switching and provides translation functionality
- Translation keys follow a hierarchical structure like `SECTION.SUBSECTION.KEY`
- The `LanguageSelectorComponent` provides a reusable UI for language selection

## How to Add a New Language

1. **Create a new translation file**:
   - Create a new file in the `src/assets/i18n/` directory named with the appropriate language code (e.g., `fr.json` for French)
   - Translate all text values while keeping the keys unchanged

2. **Add the new language to TranslateService**:
   - Open `src/app/shared/services/i18n/translation.service.ts`
   - Add the new language to the availableLanguages object:
     ```typescript
     private availableLanguages: { [key: string]: { name: string, dir: 'ltr' | 'rtl' } } = {
       'en': { name: 'English', dir: 'ltr' },
       'es': { name: 'Español', dir: 'ltr' },
       'fr': { name: 'Français', dir: 'ltr' },
       // other languages...
     };
     ```

3. **Test the new language**:
   - Run the application and test the language switcher
   - Verify all UI elements are properly translated
   - For RTL languages, verify that the layout adapts correctly
   - Note: Set `dir` to `'rtl'` for right-to-left languages like Arabic or Hebrew

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

## Using Translations

There are several ways to use translations in the application:

1. **In templates with the translate pipe**:
   ```html
   <h1>{{ 'HELLO_WORLD' | translate }}</h1>
   ```
   Or for dynamic content with parameters:
   ```html
   <p>{{ 'GREETING' | translate:{ name: userName } }}</p>
   ```

2. **In code with the TranslateService**:
   ```typescript
   // Using observable (recommended)
   this.translateService.get('WELCOME').subscribe(res => this.message = res);
   
   // Using instant translation (synchronous)
   this.message = this.translateService.instant('WELCOME');
   ```

3. **For dynamic content with parameters**:
   ```typescript
   const params = { name: 'John' };
   this.translateService.get('HELLO_NAME', params).subscribe(value => {
     this.greeting = value;
   });
   ```

## Best Practices

1. **Always use translation methods**:
   - Never hardcode user-visible strings in templates or components
   - Use the translate pipe in templates and TranslateService in components

2. **Use parameters for dynamic content**:
   ```typescript
   // In templates
   {{ 'GREETING' | translate:{ name: userName } }}
   
   // In components
   this.translateService.get('GREETING', { name: userName }).subscribe(greeting => {
     this.greeting = greeting;
   });
   ```

3. **Keep keys organized**:
   - Use a consistent naming convention for keys
   - Group related keys under meaningful parent keys
   - Document complex keys or those with parameters

4. **Handle pluralization**:
   - Use ICU message format for pluralization:
   ```
   {itemCount, plural, =0 {No items} =1 {One item} other {# items}}
   ```

5. **Test thoroughly**:
   - Always test the application in all supported languages after adding new features
   - Pay special attention to text that might expand in other languages and break layouts

## Troubleshooting

- **Missing translations**: If a translation is not found, the key or default message will be displayed
- **Language not loading**: Check that the language file exists and has the correct format
- **Text doesn't fit UI elements**: Consider using flexible layouts and avoid fixed-width containers

## Adding Translation Keys

When adding new features to the application that require user-visible text:

1. Add the appropriate keys to ALL language files
2. Follow the existing key structure and naming conventions
3. Provide accurate translations for all supported languages
4. Use descriptive keys that indicate the purpose of the text

## RTL Support

The application includes built-in support for Right-to-Left (RTL) languages like Arabic and Hebrew. Here's how RTL support is implemented:

1. **Direction attributes**: When an RTL language is selected, the `dir` attribute is automatically set to `"rtl"` on the HTML and body elements.

2. **CSS classes**: A CSS class `rtl-layout` is added to the body element for RTL languages, which allows for specific RTL styling.

3. **RTL-specific styles**: Global styles in `styles.scss` include RTL adjustments for:
   - Text alignment
   - Form elements
   - Margins and paddings
   - Directional icons and UI elements
   - Font families specific to RTL languages

4. **Component-specific RTL adjustments**: Some components have additional RTL-specific styles using CSS selectors like:
   ```scss
   :host-context([dir="rtl"]) & {
     // RTL-specific styles
   }
   ```

### Guidelines for RTL Support

When developing new components:

1. Avoid using left/right properties directly in CSS when possible; use logical properties instead:
   - Use `margin-inline-start` instead of `margin-left`
   - Use `padding-inline-end` instead of `padding-right`

2. For icons and directional elements that need to be flipped in RTL mode, use the `.icon-arrow-right` class which has built-in RTL handling.

3. Test all new UI elements in both LTR and RTL modes to ensure proper alignment and appearance.

## Lazy Loading Translations

Translations are loaded on-demand for optimal performance:

1. **Initial load**: Only the default language (English) is loaded initially.

2. **On-demand loading**: Other language files are loaded only when the user selects that language.

3. **Tracking loaded languages**: The `TranslationService` tracks which languages have been loaded to avoid redundant requests.

## Resources

- [Angular Localization Guide](https://angular.dev/guide/i18n)
- [Angular $localize API](https://angular.dev/api/localize/init/$localize)
- [RTL Styling Best Practices](https://rtlstyling.com/posts/rtl-styling)
- [CSS Logical Properties](https://developer.mozilla.org/en-US/docs/Web/CSS/CSS_Logical_Properties)
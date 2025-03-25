/**
 * Type definitions for translation keys to improve type safety and catch missing translations
 * at compile time instead of runtime.
 */

export interface AppTranslations {
  APP: {
    TITLE: string;
    SUBTITLE: string;
    WELCOME: string;
  };
  NAV: {
    HOME: string;
    DIAGRAMS: string;
    ABOUT: string;
    HELP: string;
  };
  AUTH: {
    LOGIN: string;
    LOGOUT: string;
    LOGGING_IN: string;
    LOGGING_OUT: string;
  };
  LANDING: {
    HERO: {
      TITLE: string;
      SUBTITLE: string;
      START_BUTTON: string;
      LEARN_MORE: string;
    };
    FEATURES: {
      TITLE: string;
      INTERACTIVE: {
        TITLE: string;
        DESCRIPTION: string;
      };
      SECURITY: {
        TITLE: string;
        DESCRIPTION: string;
      };
      STORAGE: {
        TITLE: string;
        DESCRIPTION: string;
      };
      COLLABORATION: {
        TITLE: string;
        DESCRIPTION: string;
      };
    };
  };
  DIAGRAM: {
    TOOLBAR: {
      NEW: string;
      OPEN: string;
      SAVE: string;
      SAVE_AS: string;
      ADD_NODE: string;
      ADD_TEXT: string;
      DELETE: string;
      TOGGLE_GRID: string;
      UNSAVED_CHANGES: string;
      UNSAVED_CHANGES_CREATE: string;
      UNSAVED_CHANGES_OPEN: string;
    };
    PICKER: {
      OPEN_TITLE: string;
      SAVE_TITLE: string;
    };
    HOME: {
      TITLE: string;
      CREATE_NEW: string;
      LOADING: string;
      EMPTY_TITLE: string;
      EMPTY_MESSAGE: string;
      CREATE_FIRST: string;
      CREATED: string;
      MODIFIED: string;
    };
    DEACTIVATE_GUARD: {
      UNSAVED_CHANGES: string;
    };
  };
  FOOTER: {
    COPYRIGHT: string;
    TERMS: string;
    PRIVACY: string;
    CONTACT: string;
  };
}

/**
 * Helper type for translating nested keys
 * Example usage: 
 * TranslateKey<AppTranslations, 'APP.TITLE'> or TranslateKey<AppTranslations, 'DIAGRAM.TOOLBAR.NEW'>
 */
export type TranslateKey<T, K extends string> = 
  K extends `${infer A}.${infer B}` 
    ? A extends keyof T 
      ? B extends keyof T[A] 
        ? T[A][B] extends string 
          ? K 
          : TranslateKey<T[A], B>
        : never 
      : never 
    : K extends keyof T 
      ? K 
      : never;

/**
 * Type for all possible translation keys as dot-notation strings
 * Example: 'APP.TITLE' | 'NAV.HOME' | etc.
 */
export type TranslationKey = TranslateKey<AppTranslations, string>;
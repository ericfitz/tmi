/// <reference types="cypress" />

declare namespace Cypress {
  interface Chainable {
    /**
     * Get the application title element
     * @example cy.getAppTitle()
     */
    getAppTitle(): Chainable<JQuery<HTMLElement>>;

    /**
     * Navigate to the diagrams page
     * @example cy.navigateToDiagrams()
     */
    navigateToDiagrams(): void;

    /**
     * Get the language selector dropdown
     * @example cy.getLanguageSelector()
     */
    getLanguageSelector(): Chainable<JQuery<HTMLElement>>;

    /**
     * Change the application language
     * @param language The language name to select
     * @example cy.changeLanguage('Español')
     */
    changeLanguage(language: string): void;
  }
}
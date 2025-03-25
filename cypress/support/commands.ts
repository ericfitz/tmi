// ***********************************************
// This example commands.js shows you how to
// create various custom commands and overwrite
// existing commands.
//
// For more comprehensive examples of custom
// commands please read more here:
// https://on.cypress.io/custom-commands
// ***********************************************

// App-specific custom commands
Cypress.Commands.add('getAppTitle', () => {
  return cy.get('app-header .app-title');
});

Cypress.Commands.add('navigateToDiagrams', () => {
  cy.contains('Diagrams').click();
});

Cypress.Commands.add('getLanguageSelector', () => {
  return cy.get('app-language-selector select');
});

Cypress.Commands.add('changeLanguage', (language: string) => {
  cy.getLanguageSelector().select(language);
});
describe('TMI Application', () => {
  beforeEach(() => {
    cy.visit('/');
  });

  it('should display app title', () => {
    cy.getAppTitle().should('contain', 'TMI');
  });

  it('should navigate to diagrams page', () => {
    cy.navigateToDiagrams();
    cy.url().should('include', '/diagrams');
  });

  it('should show language selector in header', () => {
    cy.getLanguageSelector().should('exist');
  });

  it('should be able to change language', () => {
    cy.changeLanguage('Español');
    cy.getLanguageSelector().should('have.value', 'es');
  });

  it('should not have console errors', () => {
    // This is similar to the Protractor browser logs check
    cy.window().then((win) => {
      cy.spy(win.console, 'error').as('consoleError');
    });
    
    // Navigate around to trigger potential errors
    cy.contains('About').click();
    cy.contains('Home').click();
    
    // Check if console.error was called
    cy.get('@consoleError').should('not.have.been.called');
  });
});
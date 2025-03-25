# TMI Testing Guide

This document describes how to run tests, write new tests, and maintain the test suite for the TMI application.

## Running Tests

### Quick Start

To run all tests, linting, and type checking:

```bash
./test-all.sh
```

This script will:
1. Run linting (ESLint)
2. Run TypeScript type checking
3. Execute all unit tests with code coverage
4. Display a coverage summary

### Individual Test Commands

#### Unit Tests

```bash
# Run all tests once
npm test -- --no-watch

# Run tests with coverage
npm test -- --no-watch --code-coverage

# Run a specific test file
npm test -- --include src/app/shared/services/auth/auth.service.spec.ts

# Run tests in watch mode (development)
npm test
```

#### End-to-End Tests

```bash
# Run all e2e tests headlessly
npm run e2e

# Open Cypress runner for development
npm run cypress:open
```

#### Linting

```bash
# Run linting
npm run lint

# Fix linting issues automatically
npm run lint -- --fix
```

#### Type Checking

```bash
# Run TypeScript type checking
npm run typecheck
```

## Test Structure

The test suite follows Angular's testing conventions:

### Unit Tests

- Each component, service, directive, pipe, and guard has a corresponding `.spec.ts` file
- Tests use Jasmine as the testing framework
- Angular TestBed is used for component testing
- Services are tested with dependency injection
- Mocks and spies are used to isolate units under test

### End-to-End Tests

- E2E tests are written with Cypress
- Tests use the `.cy.ts` file extension
- Custom commands are defined in `cypress/support/commands.ts`
- Cypress tests verify the application from a user's perspective

## Writing Effective Tests

### Component Tests

For component tests:

1. Test both the component class and its template
2. Verify component initialization
3. Test component interactions (inputs, outputs, event handlers)
4. Test component state changes
5. Verify DOM elements and bindings using `DebugElement` and `By.css`

Example:

```typescript
describe('MyComponent', () => {
  let component: MyComponent;
  let fixture: ComponentFixture<MyComponent>;
  let de: DebugElement;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      declarations: [MyComponent],
      imports: [/* required modules */],
      providers: [/* services */]
    }).compileComponents();

    fixture = TestBed.createComponent(MyComponent);
    component = fixture.componentInstance;
    de = fixture.debugElement;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });

  it('should display title', () => {
    const titleEl = de.query(By.css('.title'));
    expect(titleEl.nativeElement.textContent).toContain('Expected Title');
  });

  it('should handle button click', () => {
    spyOn(component, 'onClick');
    const button = de.query(By.css('button'));
    button.nativeElement.click();
    expect(component.onClick).toHaveBeenCalled();
  });
});
```

### Service Tests

For service tests:

1. Test service initialization
2. Test public methods
3. Test observable streams
4. Verify error handling
5. Mock dependencies

Example:

```typescript
describe('MyService', () => {
  let service: MyService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      imports: [HttpClientTestingModule],
      providers: [MyService]
    });

    service = TestBed.inject(MyService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should fetch data', () => {
    const mockData = { id: 1, name: 'Test' };
    
    service.getData().subscribe(data => {
      expect(data).toEqual(mockData);
    });

    const req = httpMock.expectOne('api/data');
    expect(req.request.method).toBe('GET');
    req.flush(mockData);
  });
});
```

### Testing Internationalization (i18n)

When testing components with translations:

1. Mock the `TranslateService`
2. Provide translation keys and values in the mock
3. Test language switching behavior

Example:

```typescript
// Mock translate loader
class MockTranslateLoader implements TranslateLoader {
  getTranslation(lang: string) {
    return of({
      'KEY': 'Translated value'
    });
  }
}

describe('I18nComponent', () => {
  // Test setup with TranslateModule
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [
        TranslateModule.forRoot({
          loader: { provide: TranslateLoader, useClass: MockTranslateLoader }
        })
      ]
    }).compileComponents();
    
    // Component setup
  });
  
  // Tests
});
```

## Test Coverage

The project aims for high test coverage:

- **Services**: 95%+ coverage
- **Components**: 85%+ coverage
- **Guards, Pipes, Directives**: 90%+ coverage

Coverage reports are generated when running:

```bash
npm test -- --no-watch --code-coverage
```

The report is available at `coverage/index.html`.

## Continuous Integration

Tests are run automatically in the CI pipeline on each pull request and merge to main.

- All tests must pass before merging
- Code coverage thresholds must be maintained

## Writing E2E Tests with Cypress

Cypress provides a powerful framework for end-to-end testing:

1. **Custom Commands**: Use custom commands in `cypress/support/commands.ts` for reusable actions
2. **Page Objects**: Implement the page object pattern through custom commands for maintainability
3. **Selectors**: Prefer data attributes (e.g., `data-cy`) for stable selectors
4. **Assertions**: Use Cypress chain-able assertions for better readability

Example:

```typescript
// Custom commands (page object pattern)
Cypress.Commands.add('login', (username, password) => {
  cy.visit('/login');
  cy.get('[data-cy=username]').type(username);
  cy.get('[data-cy=password]').type(password);
  cy.get('[data-cy=login-button]').click();
});

// Test
describe('Authentication', () => {
  it('should login successfully', () => {
    cy.login('testuser', 'password123');
    cy.url().should('include', '/dashboard');
    cy.get('[data-cy=welcome-message]').should('contain', 'Welcome, Test User');
  });
});
```

## Best Practices

1. **Isolated Tests**: Each test should be independent and not rely on other tests
2. **Clear Descriptions**: Use descriptive test names that explain what is being tested
3. **AAA Pattern**: Arrange, Act, Assert - set up test conditions, execute code, verify results
4. **Mock External Dependencies**: Use spies and mocks for external services
5. **Test Edge Cases**: Include tests for error conditions, boundary values, and edge cases
6. **Keep Tests Fast**: Tests should execute quickly to maintain developer productivity
7. **Maintain Test Code Quality**: Apply the same code quality standards to tests as to production code
8. **Cross-Browser Testing**: Run Cypress tests across multiple browsers when possible
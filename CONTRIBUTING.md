# Contributing to TMI

Thank you for your interest in contributing to the Threat Modeling Improved (TMI) project! This document provides guidelines for contributing code, documentation, and other improvements.

## Getting Started

### Prerequisites

- Node.js (v18 or higher)
- npm (v9 or higher)
- Angular CLI (installed globally is recommended)

### Development Setup

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/YOUR-USERNAME/tmi.git
   cd tmi
   ```
3. Install dependencies:
   ```bash
   npm install
   ```
4. Run the development server:
   ```bash
   npm start
   ```

## Development Workflow

### Branching Strategy

- `main` - Production-ready code
- `develop` - Integration branch for feature work
- Feature branches - For new features and non-trivial changes
- Hotfix branches - For urgent fixes to production

### Branch Naming

Use the following naming convention for branches:
- `feature/short-description`
- `bugfix/issue-number-short-description`
- `hotfix/issue-number-short-description`
- `docs/what-is-being-documented`

### Commit Messages

Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

Where `type` is one of:
- `feat`: A new feature
- `fix`: A bug fix
- `docs`: Documentation only changes
- `style`: Changes that do not affect the meaning of the code
- `refactor`: A code change that neither fixes a bug nor adds a feature
- `perf`: A code change that improves performance
- `test`: Adding missing tests or correcting existing tests
- `chore`: Changes to the build process or auxiliary tools

Example:
```
feat(diagram): add ability to export as PNG

Implements the PNG export feature using HTML5 Canvas.
Fixes #42
```

## Code Standards

### General Guidelines

- Follow the [Angular Style Guide](https://angular.io/guide/styleguide)
- Write self-documenting code with clear naming
- Use TypeScript's type system effectively
- Keep components focused on a single responsibility
- Document public APIs with JSDoc comments
- Follow project-specific patterns (see docs folder)

### Style Guide

The project uses ESLint and Prettier to enforce coding standards:

- Run linting:
  ```bash
  npm run lint
  ```
- Fix lint issues automatically:
  ```bash
  npm run lint -- --fix
  ```
- Format code:
  ```bash
  npm run format
  ```

### Testing Requirements

All code contributions should include appropriate tests:

- All new features must include unit tests
- Bug fixes should include tests that prevent regression
- Maintain or improve the overall code coverage percentage

Run tests:
```bash
npm test
```

Run with coverage report:
```bash
npm test -- --no-watch --code-coverage
```

## Pull Request Process

1. Create a new feature/bugfix branch from `develop`
2. Make your changes and commit them following the commit message guidelines
3. Push your branch to your fork
4. Submit a pull request to the `develop` branch of the main repository
5. Fill out the PR template with all required information
6. Request review from maintainers

### PR Requirements Checklist

- [ ] Code follows style guidelines
- [ ] Tests pass locally
- [ ] New tests added for new functionality
- [ ] Documentation updated if needed
- [ ] Commit messages follow guidelines
- [ ] Branch is up to date with develop

## Documentation

- Update documentation for any new features or changes to existing functionality
- Place documentation in the appropriate location:
  - User-facing documentation: `/docs/`
  - API/Component documentation: JSDoc comments in source code
  - Architecture decisions: `/docs/architecture.md`

## Community

### Code of Conduct

This project adheres to a Code of Conduct. By participating, you are expected to uphold this code.

### Communication

- GitHub Issues: For bug reports and feature requests
- Pull Requests: For code review discussions

## Resources

- [Angular Documentation](https://angular.dev/)
- [NgRx Documentation](https://ngrx.io/)
- [TypeScript Documentation](https://www.typescriptlang.org/docs/)

Thank you for contributing to TMI!
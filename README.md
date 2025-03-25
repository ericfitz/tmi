# tmi

Threat Modeling Improved

This project was generated using [Angular CLI](https://github.com/angular/angular-cli) version 19.2.1.

## Development server

The application now uses an Express server for both serving the Angular application and handling API requests. This ensures that security features like CSRF protection work correctly.

To start the development server:

1. Install dependencies (first time only):
   ```bash
   npm install
   npm install cookie-parser helmet express --save
   ```

2. Create an environment file (first time only):
   ```bash
   cp .env.example .env
   ```

2. For production or basic development:
   ```bash
   ./start.sh
   ```

3. For active development with auto-rebuild and server restart:
   ```bash
   ./dev.sh
   ```
   This script:
   - Watches for changes in Angular source files and rebuilds automatically
   - Restarts the Express server when server code changes
   - Installs nodemon if needed

4. Alternatively, run these commands manually:
   ```bash
   npm run build           # Build the Angular application once
   node server.js          # Start the Express server
   ```

Once the server is running, open your browser and navigate to `http://localhost:4200/`.

## Code scaffolding

Angular CLI includes powerful code scaffolding tools. To generate a new component, run:

```bash
ng generate component component-name
```

For a complete list of available schematics (such as `components`, `directives`, or `pipes`), run:

```bash
ng generate --help
```

## Building

To build the project run:

```bash
ng build
```

This will compile your project and store the build artifacts in the `dist/` directory. By default, the production build optimizes your application for performance and speed.

## Running unit tests

To execute unit tests with the [Karma](https://karma-runner.github.io) test runner, use the following command:

```bash
ng test
```

## Running end-to-end tests

For end-to-end (e2e) testing, run:

```bash
ng e2e
```

Angular CLI does not come with an end-to-end testing framework by default. You can choose one that suits your needs.

## Additional Resources

For more information on using the Angular CLI, including detailed command references, visit the [Angular CLI Overview and Command Reference](https://angular.dev/tools/cli) page.

> > > > > > > 8b5d323 (initial commit)

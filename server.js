/**
 * Express server to serve the Angular application
 * with security features (CSRF protection, CSP headers)
 */
const express = require('express');
const path = require('path');
const fs = require('fs');
const crypto = require('crypto');
const cookieParser = require('cookie-parser');
const helmet = require('helmet');

// Default values
let serverConfig = {
  port: process.env.PORT || 4200,
  host: process.env.HOST || 'localhost',
  production: process.env.NODE_ENV === 'production'
};

console.log(`Server configuration:
- Environment: ${serverConfig.production ? 'production' : 'development'}
- Host: ${serverConfig.host}
- Port: ${serverConfig.port}
`);

// Check if the dist directory exists
if (!fs.existsSync(path.join(__dirname, 'dist', 'tmi', 'browser'))) {
  console.error('Error: The "dist/tmi/browser" directory does not exist.');
  console.error('Please run "ng build" first to build the Angular application.');
  process.exit(1);
}

const app = express();

// Cookie parser for CSRF token
app.use(cookieParser());

// Configure Helmet security features
// Basic security headers
app.use(helmet({
  contentSecurityPolicy: false, // We'll set CSP separately
}));

// Custom CSP configuration
const cspDirectives = {
  defaultSrc: ["'self'"],
  scriptSrc: ["'self'", "'unsafe-inline'", "https://apis.google.com", "https://accounts.google.com", "https://www.gstatic.com", "https://jgraph.github.io"],
  styleSrc: ["'self'", "'unsafe-inline'", "https://fonts.googleapis.com", "https://accounts.google.com", "https://www.gstatic.com", "https://jgraph.github.io"],
  imgSrc: ["'self'", "data:", "https:", "blob:"],
  fontSrc: ["'self'", "https://fonts.gstatic.com", "https://www.gstatic.com"],
  connectSrc: ["'self'", "https://accounts.google.com", "https://www.googleapis.com", "https://drive.google.com", "https://jgraph.github.io"],
  frameSrc: ["'self'", "https://accounts.google.com", "https://drive.google.com"],
  objectSrc: ["'none'"],
  mediaSrc: ["'self'"],
  workerSrc: ["'self'", "blob:"],
  manifestSrc: ["'self'"]
};

// Apply CSP differently based on environment
if (serverConfig.production) {
  // Production: Enforce CSP
  app.use(
    helmet.contentSecurityPolicy({
      directives: {
        ...cspDirectives,
        // Only set upgrade-insecure-requests in production when enforcing
        upgradeInsecureRequests: [],
        reportUri: "/api/csp-report"
      }
    })
  );
} else {
  // Development: Don't enforce CSP
  // This avoids issues with development resources
  app.use(
    helmet.contentSecurityPolicy({
      directives: {
        ...cspDirectives,
        // In development, allow any source for faster iteration
        scriptSrc: ["'self'", "'unsafe-inline'", "*"],
        styleSrc: ["'self'", "'unsafe-inline'", "*"],
        imgSrc: ["'self'", "data:", "https:", "blob:", "*"],
        connectSrc: ["'self'", "*"]
      },
      reportUri: "/api/csp-report",
      reportOnly: true
    })
  );
}

// CSRF Protection middleware
app.use((req, res, next) => {
  // Skip CSRF for safe methods (GET, HEAD, OPTIONS)
  const safeMethods = ['GET', 'HEAD', 'OPTIONS'];
  if (safeMethods.includes(req.method)) {
    return next();
  }
  
  // Get or create CSRF token
  let csrfToken = req.cookies['XSRF-TOKEN'];
  
  // If no token exists, create a new one
  if (!csrfToken) {
    csrfToken = crypto.randomBytes(32).toString('hex');
    res.cookie('XSRF-TOKEN', csrfToken, {
      httpOnly: false,  // Must be accessible from JS
      secure: serverConfig.production,
      sameSite: 'strict'
    });
  }
  
  // Validate token on mutation requests
  const tokenFromHeader = req.headers['x-xsrf-token'];
  
  if (!tokenFromHeader || tokenFromHeader !== csrfToken) {
    return res.status(403).json({ 
      error: 'CSRF token validation failed',
      message: 'Invalid or missing CSRF token'
    });
  }
  
  next();
});

// CSRF Token endpoint - provides the token to the client
app.get('/api/csrf-token', (req, res) => {
  const token = crypto.randomBytes(32).toString('hex');
  res.cookie('XSRF-TOKEN', token, {
    httpOnly: false, // Must be accessible from JS
    secure: serverConfig.production,
    sameSite: 'strict'
  });
  res.status(204).end();
});

// CSP Reporting endpoint
app.post('/api/csp-report', express.json({ type: 'application/csp-report' }), (req, res) => {
  console.warn('CSP Violation:', req.body);
  res.status(204).end();
});

// Serve static files from the dist directory
app.use(express.static(path.join(__dirname, 'dist', 'tmi', 'browser')));

// For all GET requests, send back index.html
// This allows for client-side routing
app.get('/*', (req, res) => {
  // Set an initial CSRF cookie if it doesn't exist
  if (!req.cookies['XSRF-TOKEN']) {
    const token = crypto.randomBytes(32).toString('hex');
    res.cookie('XSRF-TOKEN', token, {
      httpOnly: false, // Must be accessible from JS
      secure: serverConfig.production,
      sameSite: 'strict'
    });
  }
  
  res.sendFile(path.join(__dirname, 'dist', 'tmi', 'browser', 'index.html'));
});

// Start the server
app.listen(serverConfig.port, serverConfig.host, () => {
  console.log(`Server running at http://${serverConfig.host}:${serverConfig.port}/`);
  console.log(`Environment: ${serverConfig.production ? 'Production' : 'Development'}`);
});
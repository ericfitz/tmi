/**
 * Helper module for accessing environment configuration at runtime
 * Allows for overriding environment values with URL parameters or environment variables
 */
import { environment } from '../environments/environment';

/**
 * Get the server port from environment or URL parameters
 * Priority:
 * 1. URL query parameter (port=xxxx)
 * 2. Environment variable
 * 3. environment.ts default
 */
export function getServerPort(): number {
  // Check URL parameters first
  const urlParams = new URLSearchParams(window.location.search);
  const portParam = urlParams.get('port');
  if (portParam && !isNaN(parseInt(portParam, 10))) {
    return parseInt(portParam, 10);
  }
  
  // Use environment configuration as fallback
  return environment.server.port;
}

/**
 * Get the server host from environment
 */
export function getServerHost(): string {
  return environment.server.host;
}

/**
 * Get the base URL for the application
 */
export function getBaseUrl(): string {
  const host = getServerHost();
  const port = getServerPort();
  const protocol = window.location.protocol;
  
  return `${protocol}//${host}:${port}`;
}

/**
 * Other environment helper functions can be added here
 */
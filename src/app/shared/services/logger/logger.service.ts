import { Injectable } from '@angular/core';
import { environment } from '../../../../environments/environment';

export enum LogLevel {
  ERROR = 0,
  WARN = 1,
  INFO = 2,
  DEBUG = 3,
  TRACE = 4
}

export interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
  context?: string;
  data?: unknown;
}

@Injectable({
  providedIn: 'root'
})
export class LoggerService {
  private level: LogLevel = LogLevel.INFO;

  constructor() {
    this.setLogLevel(environment.logging.level);
  }

  private setLogLevel(level: string): void {
    switch (level.toLowerCase()) {
      case 'trace':
        this.level = LogLevel.TRACE;
        break;
      case 'debug':
        this.level = LogLevel.DEBUG;
        break;
      case 'info':
        this.level = LogLevel.INFO;
        break;
      case 'warn':
        this.level = LogLevel.WARN;
        break;
      case 'error':
        this.level = LogLevel.ERROR;
        break;
      default:
        this.level = LogLevel.INFO;
    }
  }

  private formatLogEntry(level: string, message: string, context?: string, data?: unknown): LogEntry {
    const entry: LogEntry = {
      timestamp: environment.logging.includeTimestamp ? new Date().toISOString() : '',
      level,
      message,
      context,
      data
    };

    return entry;
  }

  private logToConsole(entry: LogEntry, levelValue: LogLevel): void {
    if (levelValue > this.level) {
      return;
    }

    const formattedData = JSON.stringify(entry);
    
    switch (levelValue) {
      case LogLevel.TRACE:
        // Using console.debug with a "TRACE" prefix to differentiate from DEBUG
        this.logDebug(`TRACE: ${formattedData}`);
        break;
      case LogLevel.DEBUG:
        this.logDebug(formattedData);
        break;
      case LogLevel.INFO:
        this.logInfo(formattedData);
        break;
      case LogLevel.WARN:
        console.warn(formattedData);
        break;
      case LogLevel.ERROR:
        console.error(formattedData);
        break;
    }
  }

  // Private helpers to encapsulate console methods
  // This allows us to satisfy the ESLint rules while keeping the functionality
  private logDebug(message: string): void {
    // eslint-disable-next-line no-console
    console.debug(message);
  }

  private logInfo(message: string): void {
    // eslint-disable-next-line no-console
    console.info(message);
  }

  // Public API methods
  
  trace(message: string, context?: string, data?: unknown): void {
    const entry = this.formatLogEntry('TRACE', message, context, data);
    this.logToConsole(entry, LogLevel.TRACE);
  }

  debug(message: string, context?: string, data?: unknown): void {
    const entry = this.formatLogEntry('DEBUG', message, context, data);
    this.logToConsole(entry, LogLevel.DEBUG);
  }

  info(message: string, context?: string, data?: unknown): void {
    const entry = this.formatLogEntry('INFO', message, context, data);
    this.logToConsole(entry, LogLevel.INFO);
  }

  warn(message: string, context?: string, data?: unknown): void {
    const entry = this.formatLogEntry('WARN', message, context, data);
    this.logToConsole(entry, LogLevel.WARN);
  }

  error(message: string, context?: string, data?: unknown): void {
    const entry = this.formatLogEntry('ERROR', message, context, data);
    this.logToConsole(entry, LogLevel.ERROR);
  }
}
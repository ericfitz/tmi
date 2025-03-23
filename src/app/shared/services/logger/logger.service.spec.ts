import { TestBed } from '@angular/core/testing';
import { LoggerService, LogLevel } from './logger.service';
import { environment } from '../../../../environments/environment';

describe('LoggerService', () => {
  let service: LoggerService;
  let consoleErrorSpy: jasmine.Spy;
  let consoleWarnSpy: jasmine.Spy;
  let consoleInfoSpy: jasmine.Spy;
  let consoleDebugSpy: jasmine.Spy;

  beforeEach(() => {
    // Set up spies
    consoleErrorSpy = spyOn(console, 'error');
    consoleWarnSpy = spyOn(console, 'warn');
    consoleInfoSpy = spyOn(console, 'info');
    consoleDebugSpy = spyOn(console, 'debug');

    TestBed.configureTestingModule({});
    service = TestBed.inject(LoggerService);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should log error messages at any log level', () => {
    service.error('Test error');
    expect(consoleErrorSpy).toHaveBeenCalled();
  });

  it('should log warn messages when level is warn or higher', () => {
    // Reset by calling with 'warn' level explicitly
    (service as any).setLogLevel('warn');
    
    service.warn('Test warning');
    expect(consoleWarnSpy).toHaveBeenCalled();
    
    service.info('Test info');
    expect(consoleInfoSpy).not.toHaveBeenCalled();
  });

  it('should log info messages when level is info or higher', () => {
    // Reset by calling with 'info' level explicitly
    (service as any).setLogLevel('info');
    
    service.info('Test info');
    expect(consoleInfoSpy).toHaveBeenCalled();
    
    service.debug('Test debug');
    expect(consoleDebugSpy).not.toHaveBeenCalled();
  });

  it('should include context and data in log messages', () => {
    const context = 'TestContext';
    const data = { id: 123, name: 'Test' };
    
    service.error('Test error', context, data);
    
    const loggedObject = JSON.parse((consoleErrorSpy.calls.mostRecent().args[0] as string));
    expect(loggedObject.context).toBe(context);
    expect(loggedObject.data).toEqual(data);
  });

  it('should include timestamp when configured', () => {
    const originalTimestampSetting = environment.logging.includeTimestamp;
    (environment.logging as any).includeTimestamp = true;
    
    service.error('Test with timestamp');
    
    const loggedObject = JSON.parse((consoleErrorSpy.calls.mostRecent().args[0] as string));
    expect(loggedObject.timestamp).toBeTruthy();
    
    // Reset the environment
    (environment.logging as any).includeTimestamp = originalTimestampSetting;
  });
});
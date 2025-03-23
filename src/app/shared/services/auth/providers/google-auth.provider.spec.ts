import { TestBed } from '@angular/core/testing';
import { GoogleAuthProvider } from './google-auth.provider';
import { LoggerService } from '../../logger/logger.service';

describe('GoogleAuthProvider', () => {
  let provider: GoogleAuthProvider;
  let loggerServiceSpy: jasmine.SpyObj<LoggerService>;

  beforeEach(() => {
    const spy = jasmine.createSpyObj('LoggerService', ['debug', 'info', 'error']);
    
    TestBed.configureTestingModule({
      providers: [
        GoogleAuthProvider,
        { provide: LoggerService, useValue: spy }
      ]
    });
    
    provider = TestBed.inject(GoogleAuthProvider);
    loggerServiceSpy = TestBed.inject(LoggerService) as jasmine.SpyObj<LoggerService>;
  });

  it('should be created', () => {
    expect(provider).toBeTruthy();
  });
});

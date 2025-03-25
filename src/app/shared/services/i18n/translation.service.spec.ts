import { TestBed } from '@angular/core/testing';
import { TranslateModule, TranslateLoader, TranslateService } from '@ngx-translate/core';
import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { TranslationService } from './translation.service';
import { LoggerService } from '../logger/logger.service';
import { of } from 'rxjs';

// Create a mock translate loader
class TranslateLoaderMock implements TranslateLoader {
  getTranslation(lang: string) {
    // Return mock translations
    const translations: Record<string, any> = {
      en: {
        APP: {
          TITLE: 'TMI',
          SUBTITLE: 'Threat Modeling Improved'
        }
      },
      es: {
        APP: {
          TITLE: 'TMI',
          SUBTITLE: 'Modelado de Amenazas Mejorado'
        }
      }
    };
    
    return of(translations[lang] || translations['en']);
  }
}

describe('TranslationService', () => {
  let service: TranslationService;
  let translateService: TranslateService;
  let httpMock: HttpTestingController;
  let loggerSpy: jasmine.SpyObj<LoggerService>;

  beforeEach(() => {
    // Create spy for logger service
    const spy = jasmine.createSpyObj('LoggerService', ['debug', 'info', 'error']);
    
    TestBed.configureTestingModule({
      imports: [
        HttpClientTestingModule,
        TranslateModule.forRoot({
          loader: {
            provide: TranslateLoader,
            useClass: TranslateLoaderMock
          }
        })
      ],
      providers: [
        TranslationService,
        { provide: LoggerService, useValue: spy }
      ]
    });
    
    service = TestBed.inject(TranslationService);
    translateService = TestBed.inject(TranslateService);
    httpMock = TestBed.inject(HttpTestingController);
    loggerSpy = TestBed.inject(LoggerService) as jasmine.SpyObj<LoggerService>;
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should initialize with default language', () => {
    expect(translateService.defaultLang).toBe('en');
  });

  it('should change language', (done) => {
    service.changeLanguage('es').subscribe(() => {
      expect(translateService.currentLang).toBe('es');
      done();
    });
  });

  it('should get translations', (done) => {
    service.get('APP.TITLE').subscribe(translation => {
      expect(translation).toBe('TMI');
      done();
    });
  });

  it('should get instant translations', () => {
    translateService.setTranslation('en', { APP: { TITLE: 'TMI' } });
    expect(service.instant('APP.TITLE')).toBe('TMI');
  });

  it('should fallback to default language if requested language not available', (done) => {
    // Override the getLanguage method to return an unsupported language
    spyOn<any>(service, 'getLanguage').and.returnValue('fr');
    
    service.initialize().then(() => {
      expect(loggerSpy.error).toHaveBeenCalled();
      expect(translateService.currentLang).toBe('en');
      done();
    });
  });
});
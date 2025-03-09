import { ComponentFixture, TestBed } from '@angular/core/testing';

import { DiagramToolbarComponent } from './diagram-toolbar.component';

describe('DiagramToolbarComponent', () => {
  let component: DiagramToolbarComponent;
  let fixture: ComponentFixture<DiagramToolbarComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [DiagramToolbarComponent]
    })
    .compileComponents();

    fixture = TestBed.createComponent(DiagramToolbarComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });
});

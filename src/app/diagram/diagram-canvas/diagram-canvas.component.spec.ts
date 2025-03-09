import { ComponentFixture, TestBed } from '@angular/core/testing';

import { DiagramCanvasComponent } from './diagram-canvas.component';

describe('DiagramCanvasComponent', () => {
  let component: DiagramCanvasComponent;
  let fixture: ComponentFixture<DiagramCanvasComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [DiagramCanvasComponent]
    })
    .compileComponents();

    fixture = TestBed.createComponent(DiagramCanvasComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });
});

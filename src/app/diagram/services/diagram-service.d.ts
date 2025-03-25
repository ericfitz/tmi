import { Observable } from 'rxjs';
import { StorageFile } from '../../shared/services/storage/providers/storage-provider.interface';

export interface DiagramCell {
  id: string;
  value: string;
  isVertex: boolean;
  isEdge: boolean;
  geometry: {
    x: number;
    y: number;
    width: number;
    height: number;
  };
  style?: any; // Style can be string or object depending on the graph library
  sourceId?: string;
  targetId?: string;
}

export interface DiagramData {
  id?: string;
  title: string;
  cells: DiagramCell[];
  createdAt?: string;
  updatedAt?: string;
  version?: string;
  properties?: Record<string, unknown>;
  selectedCellId?: string; // Added to track the currently selected cell
}

export interface DiagramService {
  // Core graph initialization
  initGraph(container: HTMLElement): any;
  resetDiagram(): void;

  // Element operations
  addNode(x: number, y: number, width: number, height: number, label: string, style?: string): any;
  addEdge(source: any, target: any, label?: string, style?: string): any;
  deleteSelected(): void;

  // Diagram state
  getCurrentDiagram(): DiagramData | null;
  isDiagramDirty(): boolean;
  markDiagramDirty(): void;
  markDiagramClean(): void;

  // File operations
  getCurrentFile(): StorageFile | null;
  exportDiagram(): DiagramData;
  importDiagram(data: DiagramData): void;
  saveDiagram(filename?: string): Promise<StorageFile>;
  loadDiagram(id: string): Promise<void>;
  loadDiagramList(): Promise<StorageFile[]>;

  // Observable state
  currentDiagram$: Observable<DiagramData | null>;
  isDirty$: Observable<boolean>;
  currentFile$: Observable<StorageFile | null>;
}
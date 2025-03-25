export interface Position {
  x: number;
  y: number;
}

export interface Size {
  width: number;
  height: number;
}

export interface DiagramElement {
  id: string;
  type: DiagramElementType;
  position: Position;
  size: Size;
  properties: DiagramElementProperties;
  zIndex: number;
}

export enum DiagramElementType {
  RECTANGLE = 'rectangle',
  CIRCLE = 'circle',
  TRIANGLE = 'triangle',
  TEXT = 'text',
  LINE = 'line',
  IMAGE = 'image',
  CONNECTOR = 'connector'
}

export interface DiagramElementProperties {
  text?: string;
  color?: string;
  backgroundColor?: string;
  borderColor?: string;
  borderWidth?: number;
  opacity?: number;
  fontFamily?: string;
  fontSize?: number;
  fontWeight?: string;
  fontStyle?: string;
  textAlign?: string;
  rotation?: number;
  imageUrl?: string;
  sourceElementId?: string;
  targetElementId?: string;
  points?: Position[];
  [key: string]: string | number | boolean | Position[] | undefined;
}

export interface Diagram {
  id: string;
  name: string;
  elements: DiagramElement[];
  createdAt: string;
  updatedAt: string;
  version: number;
  properties: DiagramProperties;
}

export interface DiagramProperties {
  backgroundColor?: string;
  gridSize?: number;
  snapToGrid?: boolean;
  width?: number;
  height?: number;
  [key: string]: string | number | boolean | undefined;
}

export interface DiagramHistoryItem {
  timestamp: string;
  action: string;
  elements: DiagramElement[];
}

export interface DiagramMetadata {
  id: string;
  name: string;
  thumbnail?: string;
  createdAt: string;
  updatedAt: string;
  lastOpenedAt?: string;
}

export interface DiagramFile {
  diagram: Diagram;
  history: DiagramHistoryItem[];
}

/**
 * Cell geometry type definition
 */
export interface CellGeometry {
  x: number;
  y: number;
  width: number;
  height: number;
  points?: Position[];
  relative?: boolean;
  offset?: Position;
}

/**
 * Graph cell definition
 */
export interface GraphCell {
  id: string;
  value: string;
  style?: string;
  geometry?: CellGeometry;
  edge?: boolean;
  vertex?: boolean;
  connectable?: boolean;
  parent?: GraphCell;
  source?: GraphCell;
  target?: GraphCell;
  children?: GraphCell[];
}

/**
 * Graph view interface
 */
export interface GraphView {
  getScale: () => number;
  setScale: (scale: number) => void;
  refresh: () => void;
}

/**
 * Graph model interface
 */
export interface GraphModel {
  beginUpdate: () => void; 
  endUpdate: () => void;
  getCell: (id: string) => GraphCell;
  setValue: (cell: GraphCell, value: string) => void;
  setStyle: (cell: GraphCell, style: string) => void;
  setGeometry: (cell: GraphCell, geometry: CellGeometry) => void;
  getChildCells: (parent: GraphCell) => GraphCell[];
  getChildCount: (parent: GraphCell) => number;
}

/**
 * Defines the diagram graph interface for type safety
 */
export interface DiagramGraph {
  getView: () => GraphView;
  model: GraphModel;
  getDefaultParent: () => GraphCell;
  getChildCells: (parent: GraphCell) => GraphCell[];
  isGridEnabled: () => boolean;
  setGridEnabled: (enabled: boolean) => void;
  zoomTo: (scale: number) => void;
  clearSelection: () => void;
  setSelectionCells: (cells: GraphCell[]) => void;
  removeCells: (cells: GraphCell[]) => void;
  insertVertex: (
    parent: GraphCell,
    id: string,
    value: string,
    x: number,
    y: number, 
    width: number, 
    height: number, 
    style: string
  ) => GraphCell;
  insertEdge: (
    parent: GraphCell,
    id: string, 
    value: string, 
    source: GraphCell, 
    target: GraphCell, 
    style: string
  ) => GraphCell;
  container: { style: { backgroundColor: string } };
  sizeDidChange: () => void;
  gridSize: number;
}
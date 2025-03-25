/**
 * Common type definitions used throughout the application
 */

/**
 * A type representing any valid JSON primitive value
 */
export type JsonPrimitive = string | number | boolean | null;

/**
 * A type that can be used for generic record/object structures
 * where the keys are strings and values can be of any primitive or object type
 */
export type JsonObject = {
  readonly [key: string]: JsonValue;
};

/**
 * A type representing a JSON array
 */
export type JsonArray = ReadonlyArray<JsonValue>;

/**
 * A type representing any valid JSON value
 */
export type JsonValue = JsonPrimitive | JsonObject | JsonArray;

/**
 * A type for error details with specific properties
 */
export interface ErrorDetails {
  requestId?: string;
  code?: string;
  service?: string;
  timestamp?: string;
  source?: string;
  path?: string;
  [key: string]: JsonValue | undefined;
}

/**
 * A type for error objects with improved type safety
 */
export interface ErrorResponse {
  message: string;
  code?: string;
  status?: number;
  details?: ErrorDetails;
}

/**
 * Generic event data type with improved type safety
 */
export interface EventData {
  [key: string]: JsonValue;
}

/**
 * A type for representing generic event objects with improved type safety
 */
export interface EventObject {
  type: string;
  target?: unknown;
  data?: EventData;
  timestamp?: number;
}

/**
 * A type representing a callback function with specific parameters
 */
export type Callback<T = void, P extends unknown[] = unknown[]> = (...args: P) => T;

/**
 * Common callback types used in the application
 */
export type ErrorCallback = Callback<void, [Error | ErrorResponse]>;
export type SuccessCallback<T> = Callback<void, [T]>;
export type CompletionCallback<T> = Callback<void, [Error | null, T | null]>;

/**
 * Dictionary type for key-value pairs
 */
export interface Dictionary<T> {
  [key: string]: T;
}
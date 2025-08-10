# Bug Report: asyncapi-codegen v0.46.2 - Schema Parsing Errors

## Summary
The asyncapi-codegen tool fails to parse valid AsyncAPI v3.0 schemas with two specific errors related to `additionalProperties` and `required` field handling.

## Environment
- **Tool Version**: asyncapi-codegen v0.46.2
- **AsyncAPI Version**: 3.0.0
- **Go Version**: 1.24.2
- **OS**: macOS (Darwin 24.6.0)

## Error 1: additionalProperties Boolean Values

### Error Message
```
Error: json: cannot unmarshal bool into Go struct field Schema.components.schemas.additionalProperties of type asyncapiv3.Schema
```

### Problematic Schema Pattern
```yaml
DiagramOperationPayload:
  type: object
  properties:
    message_type:
      const: diagram_operation
    user_id:
      type: string
  required:
    - message_type
    - user_id
  additionalProperties: false  # <-- This causes the error
```

### AsyncAPI v3.0 Documentation Reference
According to the [AsyncAPI v3.0 Schema Object specification](https://spec.asyncapi.com/v3.0.0#schemaObject), `additionalProperties` can be either:
- A boolean value (`true` or `false`)
- A Schema Object

From the spec:
> **additionalProperties**: boolean | Schema Object
> 
> The value of this keyword MUST be either a boolean or a schema.

The tool incorrectly expects only Schema Objects, but boolean values are explicitly allowed per the specification.

## Error 2: required Field Array Parsing

### Error Message
```
Error: json: cannot unmarshal bool into Go struct field Schema.components.schemas.Validations.required of type string
```

### Problematic Schema Pattern
```yaml
Cell:
  type: object
  properties:
    id:
      type: string
    shape:
      type: string
  required:        # <-- This array format causes the error
    - id
    - shape
```

### AsyncAPI v3.0 Documentation Reference
According to the [AsyncAPI v3.0 Schema Object specification](https://spec.asyncapi.com/v3.0.0#schemaObject), the `required` field should be:
> **required**: [string]
> 
> The value of this keyword MUST be an array. Elements of this array, if any, MUST be strings, and MUST be unique.

The tool appears to expect `required` as a single string rather than an array of strings, which contradicts the specification.

## Reproduction Steps

1. Create an AsyncAPI v3.0 specification with either:
   - `additionalProperties: false` in a schema
   - `required: [field1, field2]` array in a schema

2. Run: `asyncapi-codegen -i specification.yaml -p asyncapi -o output.go`

3. Observe the parsing errors described above

## Expected Behavior
The tool should successfully parse valid AsyncAPI v3.0 schemas that use:
- Boolean values for `additionalProperties` 
- Array format for `required` fields

## Minimal Reproduction Case

```yaml
asyncapi: '3.0.0'
info:
  title: Bug Reproduction
  version: '1.0.0'

channels:
  '/test':
    address: '/test'
    messages:
      testMessage:
        payload:
          type: object
          properties:
            id:
              type: string
          required:
            - id
          additionalProperties: false
```

Running `asyncapi-codegen -i reproduction.yaml -p test -o output.go` produces both errors.

## Impact
This prevents the tool from being used with standard AsyncAPI v3.0 specifications that follow the documented schema patterns, limiting its usability for AsyncAPI v3.0 adoption.

## Workaround Attempted
Removing `additionalProperties: false` lines resolves the first error but causes YAML syntax issues and doesn't address the fundamental `required` field parsing problem.

## Request
Please update the schema parsing logic to correctly handle:
1. Boolean values for `additionalProperties` as specified in AsyncAPI v3.0
2. Array format for `required` fields as specified in AsyncAPI v3.0

Thank you for maintaining this valuable tool!
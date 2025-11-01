# TmiClient.Node

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**shape** | **String** | Node type determining its visual representation and behavior | [optional] 
**position** | [**NodePosition**](NodePosition.md) |  | 
**size** | [**NodeSize**](NodeSize.md) |  | 
**angle** | **Number** | Rotation angle in degrees | [optional] [default to 0]
**attrs** | **Object** | Visual styling attributes for the node | [optional] 
**ports** | **Object** | Port configuration for connections | [optional] 
**parent** | **String** | ID of the parent cell for nested/grouped nodes (UUID) | [optional] 

<a name="ShapeEnum"></a>
## Enum: ShapeEnum

* `actor` (value: `"actor"`)
* `process` (value: `"process"`)
* `store` (value: `"store"`)
* `securityBoundary` (value: `"security-boundary"`)
* `textBox` (value: `"text-box"`)


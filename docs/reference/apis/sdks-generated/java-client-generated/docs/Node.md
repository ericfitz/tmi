# Node

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**nodeShape** | [**ShapeEnum**](#ShapeEnum) | Node type determining its visual representation and behavior |  [optional]
**position** | [**NodePosition**](NodePosition.md) |  | 
**size** | [**NodeSize**](NodeSize.md) |  | 
**angle** | [**BigDecimal**](BigDecimal.md) | Rotation angle in degrees |  [optional]
**attrs** | **Object** | Visual styling attributes for the node |  [optional]
**ports** | **Object** | Port configuration for connections |  [optional]
**parent** | [**UUID**](UUID.md) | ID of the parent cell for nested/grouped nodes (UUID) |  [optional]

<a name="ShapeEnum"></a>
## Enum: ShapeEnum
Name | Value
---- | -----
ACTOR | &quot;actor&quot;
PROCESS | &quot;process&quot;
STORE | &quot;store&quot;
SECURITY_BOUNDARY | &quot;security-boundary&quot;
TEXT_BOX | &quot;text-box&quot;

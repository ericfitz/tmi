# Node

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Shape** | **string** | Node type determining its visual representation and behavior | [default to null]
**Position** | [***NodePosition**](Node_position.md) |  | [default to null]
**Size** | [***NodeSize**](Node_size.md) |  | [default to null]
**Angle** | **float64** | Rotation angle in degrees | [optional] [default to 0]
**Attrs** | [***Object**](.md) | Visual styling attributes for the node | [optional] [default to null]
**Ports** | [***Object**](.md) | Port configuration for connections | [optional] [default to null]
**Parent** | **string** | ID of the parent cell for nested/grouped nodes (UUID) | [optional] [default to null]
**Id** | **string** | Unique identifier of the cell (UUID) | [default to null]
**ZIndex** | **float64** | Z-order layer for rendering (higher values render on top) | [optional] [default to 1]
**Visible** | **bool** | Whether the cell is visible in the diagram | [optional] [default to true]
**Data** | [***CellData**](Cell_data.md) |  | [optional] [default to null]
**Markup** | [**[]MarkupElement**](MarkupElement.md) | SVG/HTML markup definition for custom shape rendering in X6 | [optional] [default to null]
**Tools** | [**[]CellTool**](CellTool.md) | Interactive tools attached to the cell for user interaction | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


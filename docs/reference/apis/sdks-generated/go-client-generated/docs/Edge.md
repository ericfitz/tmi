# Edge

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Shape** | **string** | Edge type identifier | [default to null]
**Source** | [***Object**](.md) | Source connection point | [default to null]
**Target** | [***Object**](.md) | Target connection point | [default to null]
**Attrs** | [***Object**](.md) | Visual styling attributes for the edge | [optional] [default to null]
**Labels** | [**[]EdgeLabel**](EdgeLabel.md) | Text labels positioned along the edge | [optional] [default to null]
**Vertices** | [**[]Point**](Point.md) | Intermediate waypoints for edge routing | [optional] [default to null]
**Router** | [***Object**](.md) | Edge routing algorithm configuration for path calculation | [optional] [default to null]
**Connector** | [***Object**](.md) | Edge connector style configuration for visual appearance | [optional] [default to null]
**DefaultLabel** | [***Object**](.md) | Default label configuration applied to edges without explicit labels | [optional] [default to null]
**Id** | **string** | Unique identifier of the cell (UUID) | [default to null]
**ZIndex** | **float64** | Z-order layer for rendering (higher values render on top) | [optional] [default to 1]
**Visible** | **bool** | Whether the cell is visible in the diagram | [optional] [default to true]
**Data** | [***CellData**](Cell_data.md) |  | [optional] [default to null]
**Markup** | [**[]MarkupElement**](MarkupElement.md) | SVG/HTML markup definition for custom shape rendering in X6 | [optional] [default to null]
**Tools** | [**[]CellTool**](CellTool.md) | Interactive tools attached to the cell for user interaction | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


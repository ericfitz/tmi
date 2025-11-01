# Cell

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Id** | **string** | Unique identifier of the cell (UUID) | [default to null]
**Shape** | **string** | Shape type identifier that determines cell structure and behavior | [default to null]
**ZIndex** | **float64** | Z-order layer for rendering (higher values render on top) | [optional] [default to 1]
**Visible** | **bool** | Whether the cell is visible in the diagram | [optional] [default to true]
**Data** | [***CellData**](Cell_data.md) |  | [optional] [default to null]
**Markup** | [**[]MarkupElement**](MarkupElement.md) | SVG/HTML markup definition for custom shape rendering in X6 | [optional] [default to null]
**Tools** | [**[]CellTool**](CellTool.md) | Interactive tools attached to the cell for user interaction | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


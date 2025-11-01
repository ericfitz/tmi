# Cell

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **str** | Unique identifier of the cell (UUID) | 
**shape** | **str** | Shape type identifier that determines cell structure and behavior | 
**z_index** | **float** | Z-order layer for rendering (higher values render on top) | [optional] [default to 1]
**visible** | **bool** | Whether the cell is visible in the diagram | [optional] [default to True]
**data** | [**CellData**](CellData.md) |  | [optional] 
**markup** | [**list[MarkupElement]**](MarkupElement.md) | SVG/HTML markup definition for custom shape rendering in X6 | [optional] 
**tools** | [**list[CellTool]**](CellTool.md) | Interactive tools attached to the cell for user interaction | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


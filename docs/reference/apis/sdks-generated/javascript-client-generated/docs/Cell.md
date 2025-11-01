# TmiClient.Cell

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **String** | Unique identifier of the cell (UUID) | 
**shape** | **String** | Shape type identifier that determines cell structure and behavior | 
**zIndex** | **Number** | Z-order layer for rendering (higher values render on top) | [optional] [default to 1]
**visible** | **Boolean** | Whether the cell is visible in the diagram | [optional] [default to true]
**data** | [**CellData**](CellData.md) |  | [optional] 
**markup** | [**[MarkupElement]**](MarkupElement.md) | SVG/HTML markup definition for custom shape rendering in X6 | [optional] 
**tools** | [**[CellTool]**](CellTool.md) | Interactive tools attached to the cell for user interaction | [optional] 

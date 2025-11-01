# Cell

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | [**UUID**](UUID.md) | Unique identifier of the cell (UUID) | 
**shape** | **String** | Shape type identifier that determines cell structure and behavior | 
**zIndex** | [**BigDecimal**](BigDecimal.md) | Z-order layer for rendering (higher values render on top) |  [optional]
**visible** | **Boolean** | Whether the cell is visible in the diagram |  [optional]
**data** | [**Map**](Map.md) |  |  [optional]
**markup** | [**List&lt;MarkupElement&gt;**](MarkupElement.md) | SVG/HTML markup definition for custom shape rendering in X6 |  [optional]
**tools** | [**List&lt;CellTool&gt;**](CellTool.md) | Interactive tools attached to the cell for user interaction |  [optional]

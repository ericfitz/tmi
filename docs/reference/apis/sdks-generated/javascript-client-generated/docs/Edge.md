# TmiClient.Edge

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**shape** | **String** | Edge type identifier | [optional] 
**source** | **Object** | Source connection point | 
**target** | **Object** | Target connection point | 
**attrs** | **Object** | Visual styling attributes for the edge | [optional] 
**labels** | [**[EdgeLabel]**](EdgeLabel.md) | Text labels positioned along the edge | [optional] 
**vertices** | [**[Point]**](Point.md) | Intermediate waypoints for edge routing | [optional] 
**router** | **Object** | Edge routing algorithm configuration for path calculation | [optional] 
**connector** | **Object** | Edge connector style configuration for visual appearance | [optional] 
**defaultLabel** | **Object** | Default label configuration applied to edges without explicit labels | [optional] 

<a name="ShapeEnum"></a>
## Enum: ShapeEnum

* `edge` (value: `"edge"`)


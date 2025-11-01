# Edge

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**edgeShape** | [**ShapeEnum**](#ShapeEnum) | Edge type identifier |  [optional]
**source** | **Object** | Source connection point | 
**target** | **Object** | Target connection point | 
**attrs** | **Object** | Visual styling attributes for the edge |  [optional]
**labels** | [**List&lt;EdgeLabel&gt;**](EdgeLabel.md) | Text labels positioned along the edge |  [optional]
**vertices** | [**List&lt;Point&gt;**](Point.md) | Intermediate waypoints for edge routing |  [optional]
**router** | **Object** | Edge routing algorithm configuration for path calculation |  [optional]
**connector** | **Object** | Edge connector style configuration for visual appearance |  [optional]
**defaultLabel** | **Object** | Default label configuration applied to edges without explicit labels |  [optional]

<a name="ShapeEnum"></a>
## Enum: ShapeEnum
Name | Value
---- | -----
EDGE | &quot;edge&quot;

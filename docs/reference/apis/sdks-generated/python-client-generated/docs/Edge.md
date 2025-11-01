# Edge

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**shape** | **str** | Edge type identifier | [optional] 
**source** | **object** | Source connection point | 
**target** | **object** | Target connection point | 
**attrs** | **object** | Visual styling attributes for the edge | [optional] 
**labels** | [**list[EdgeLabel]**](EdgeLabel.md) | Text labels positioned along the edge | [optional] 
**vertices** | [**list[Point]**](Point.md) | Intermediate waypoints for edge routing | [optional] 
**router** | **object** | Edge routing algorithm configuration for path calculation | [optional] 
**connector** | **object** | Edge connector style configuration for visual appearance | [optional] 
**default_label** | **object** | Default label configuration applied to edges without explicit labels | [optional] 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


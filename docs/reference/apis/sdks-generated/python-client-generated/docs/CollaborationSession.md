# CollaborationSession

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**session_id** | **str** | Unique identifier for the session (UUID) | 
**host** | **str** | Email address of the session host | 
**presenter** | **str** | Email address of the current presenter | [optional] 
**threat_model_id** | **str** | Unique identifier of the associated threat model (UUID) | 
**threat_model_name** | **str** | Name of the associated threat model | 
**diagram_id** | **str** | Unique identifier of the associated diagram (UUID) | 
**diagram_name** | **str** | Name of the associated diagram | 
**participants** | [**list[Participant]**](Participant.md) | List of active participants | 
**websocket_url** | **str** | WebSocket URL for real-time updates | 

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


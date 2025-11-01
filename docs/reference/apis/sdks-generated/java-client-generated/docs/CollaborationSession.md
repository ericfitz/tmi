# CollaborationSession

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**sessionId** | [**UUID**](UUID.md) | Unique identifier for the session (UUID) | 
**host** | **String** | Email address of the session host | 
**presenter** | **String** | Email address of the current presenter |  [optional]
**threatModelId** | [**UUID**](UUID.md) | Unique identifier of the associated threat model (UUID) | 
**threatModelName** | **String** | Name of the associated threat model | 
**diagramId** | [**UUID**](UUID.md) | Unique identifier of the associated diagram (UUID) | 
**diagramName** | **String** | Name of the associated diagram | 
**participants** | [**List&lt;Participant&gt;**](Participant.md) | List of active participants | 
**websocketUrl** | **String** | WebSocket URL for real-time updates | 

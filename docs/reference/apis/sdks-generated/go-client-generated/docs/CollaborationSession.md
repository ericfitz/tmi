# CollaborationSession

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**SessionId** | **string** | Unique identifier for the session (UUID) | [default to null]
**Host** | **string** | Email address of the session host | [default to null]
**Presenter** | **string** | Email address of the current presenter | [optional] [default to null]
**ThreatModelId** | **string** | Unique identifier of the associated threat model (UUID) | [default to null]
**ThreatModelName** | **string** | Name of the associated threat model | [default to null]
**DiagramId** | **string** | Unique identifier of the associated diagram (UUID) | [default to null]
**DiagramName** | **string** | Name of the associated diagram | [default to null]
**Participants** | [**[]Participant**](Participant.md) | List of active participants | [default to null]
**WebsocketUrl** | **string** | WebSocket URL for real-time updates | [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


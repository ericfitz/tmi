# TmiClient.ThreatModel

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **String** | Unique identifier for the threat model (UUID) | [optional] 
**createdAt** | **Date** | Creation timestamp (RFC3339) | [optional] 
**modifiedAt** | **Date** | Last modification timestamp (RFC3339) | [optional] 
**createdBy** | **String** | User name, email or identifier of the creator of the threat model | [optional] 
**documents** | [**[Document]**](Document.md) | List of documents related to the threat model | [optional] 
**repositories** | [**[Repository]**](Repository.md) | List of source code repositories related to the threat model | [optional] 
**diagrams** | [**[Diagram]**](Diagram.md) | List of diagram objects associated with this threat model | [optional] 
**threats** | [**[Threat]**](Threat.md) | List of threats within the threat model | [optional] 
**notes** | [**[Note]**](Note.md) | List of notes associated with the threat model | [optional] 
**assets** | [**[ExtendedAsset]**](ExtendedAsset.md) | List of assets associated with the threat model | [optional] 
**statusUpdated** | **Date** | Timestamp when the status field was last modified (RFC3339). Automatically updated by the server when status changes. | [optional] 

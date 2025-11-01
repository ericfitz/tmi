# Authorization

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**Subject** | **string** | Email address or user id for users, group name for groups | [default to null]
**SubjectType** | **string** | Type of authorization subject: user (individual) or group | [default to null]
**Idp** | **string** | Identity provider (required for groups, optional for users) | [optional] [default to null]
**Role** | **string** | Role: reader (view), writer (edit), owner (full control) | [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


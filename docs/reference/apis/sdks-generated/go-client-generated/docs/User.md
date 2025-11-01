# User

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**UserId** | **string** | OAuth provider&#x27;s unique identifier for the user (from primary provider) | [default to null]
**Email** | **string** | User&#x27;s email address | [default to null]
**Name** | **string** | User&#x27;s display name | [default to null]
**Picture** | **string** | URL to user&#x27;s profile picture | [optional] [default to null]
**Idp** | **string** | Identity provider used for current session | [optional] [default to null]
**Groups** | **[]string** | Groups the user belongs to (from identity provider) | [optional] [default to null]
**LastLogin** | [**time.Time**](time.Time.md) | Timestamp of user&#x27;s last login | [optional] [default to null]

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


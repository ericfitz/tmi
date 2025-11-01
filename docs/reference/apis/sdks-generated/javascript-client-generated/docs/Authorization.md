# TmiClient.Authorization

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**subject** | **String** | Email address or user id for users, group name for groups | 
**subjectType** | **String** | Type of authorization subject: user (individual) or group | 
**idp** | **String** | Identity provider (required for groups, optional for users) | [optional] 
**role** | **String** | Role: reader (view), writer (edit), owner (full control) | 

<a name="SubjectTypeEnum"></a>
## Enum: SubjectTypeEnum

* `user` (value: `"user"`)
* `group` (value: `"group"`)


<a name="RoleEnum"></a>
## Enum: RoleEnum

* `reader` (value: `"reader"`)
* `writer` (value: `"writer"`)
* `owner` (value: `"owner"`)


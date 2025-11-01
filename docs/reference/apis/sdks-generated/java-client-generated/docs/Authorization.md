# Authorization

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**subject** | **String** | Email address or user id for users, group name for groups | 
**subjectType** | [**SubjectTypeEnum**](#SubjectTypeEnum) | Type of authorization subject: user (individual) or group | 
**idp** | **String** | Identity provider (required for groups, optional for users) |  [optional]
**role** | [**RoleEnum**](#RoleEnum) | Role: reader (view), writer (edit), owner (full control) | 

<a name="SubjectTypeEnum"></a>
## Enum: SubjectTypeEnum
Name | Value
---- | -----
USER | &quot;user&quot;
GROUP | &quot;group&quot;

<a name="RoleEnum"></a>
## Enum: RoleEnum
Name | Value
---- | -----
READER | &quot;reader&quot;
WRITER | &quot;writer&quot;
OWNER | &quot;owner&quot;

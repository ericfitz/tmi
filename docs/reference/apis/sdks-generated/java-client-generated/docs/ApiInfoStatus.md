# ApiInfoStatus

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**code** | [**CodeEnum**](#CodeEnum) | Status code indicating if the API is functioning correctly | 
**time** | [**OffsetDateTime**](OffsetDateTime.md) | Current server time in UTC, formatted as RFC 3339 | 

<a name="CodeEnum"></a>
## Enum: CodeEnum
Name | Value
---- | -----
OK | &quot;OK&quot;
ERROR | &quot;ERROR&quot;

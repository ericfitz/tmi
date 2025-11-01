# ThreatModelsThreatModelIdBody

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**op** | [**OpEnum**](#OpEnum) | Patch operation type | 
**path** | **String** | JSON path to target | 
**value** | **Object** | Value to apply |  [optional]

<a name="OpEnum"></a>
## Enum: OpEnum
Name | Value
---- | -----
ADD | &quot;add&quot;
REPLACE | &quot;replace&quot;
REMOVE | &quot;remove&quot;
MOVE | &quot;move&quot;
COPY | &quot;copy&quot;
TEST | &quot;test&quot;

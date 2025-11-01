# RepositoryBaseParameters

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**refType** | [**RefTypeEnum**](#RefTypeEnum) | Reference type (branch, tag, or commit) | 
**refValue** | **String** | Reference value (branch name, tag value, or commit id) | 
**subPath** | **String** | Sub-path within the repository |  [optional]

<a name="RefTypeEnum"></a>
## Enum: RefTypeEnum
Name | Value
---- | -----
BRANCH | &quot;branch&quot;
TAG | &quot;tag&quot;
COMMIT | &quot;commit&quot;

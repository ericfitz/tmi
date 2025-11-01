# RepositoryBase

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **String** | Name for the source code reference |  [optional]
**description** | **String** | Description of the referenced source code |  [optional]
**type** | [**TypeEnum**](#TypeEnum) | Source code repository type |  [optional]
**parameters** | [**RepositoryBaseParameters**](RepositoryBaseParameters.md) |  |  [optional]
**uri** | **String** | URL to retrieve the referenced source code | 

<a name="TypeEnum"></a>
## Enum: TypeEnum
Name | Value
---- | -----
GIT | &quot;git&quot;
SVN | &quot;svn&quot;
MERCURIAL | &quot;mercurial&quot;
OTHER | &quot;other&quot;

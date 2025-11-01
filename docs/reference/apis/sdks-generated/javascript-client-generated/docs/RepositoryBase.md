# TmiClient.RepositoryBase

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **String** | Name for the source code reference | [optional] 
**description** | **String** | Description of the referenced source code | [optional] 
**type** | **String** | Source code repository type | [optional] 
**parameters** | [**RepositoryBaseParameters**](RepositoryBaseParameters.md) |  | [optional] 
**uri** | **String** | URL to retrieve the referenced source code | 

<a name="TypeEnum"></a>
## Enum: TypeEnum

* `git` (value: `"git"`)
* `svn` (value: `"svn"`)
* `mercurial` (value: `"mercurial"`)
* `other` (value: `"other"`)


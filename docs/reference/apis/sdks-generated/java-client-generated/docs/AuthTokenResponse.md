# AuthTokenResponse

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**accessToken** | **String** | JWT access token | 
**refreshToken** | **String** | Refresh token for obtaining new access tokens | 
**tokenType** | [**TokenTypeEnum**](#TokenTypeEnum) | Token type | 
**expiresIn** | **Integer** | Access token expiration time in seconds | 

<a name="TokenTypeEnum"></a>
## Enum: TokenTypeEnum
Name | Value
---- | -----
BEARER | &quot;Bearer&quot;

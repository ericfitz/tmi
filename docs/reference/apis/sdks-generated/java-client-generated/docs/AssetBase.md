# AssetBase

## Properties
Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **String** | Asset name | 
**description** | **String** | Description of the asset |  [optional]
**type** | [**TypeEnum**](#TypeEnum) | Type of asset | 
**criticality** | **String** | Criticality level of the asset |  [optional]
**classification** | **List&lt;String&gt;** | Classification tags for the asset |  [optional]
**sensitivity** | **List&lt;String&gt;** | Sensitivity labels for the asset |  [optional]

<a name="TypeEnum"></a>
## Enum: TypeEnum
Name | Value
---- | -----
DATA | &quot;data&quot;
HARDWARE | &quot;hardware&quot;
SOFTWARE | &quot;software&quot;
INFRASTRUCTURE | &quot;infrastructure&quot;
SERVICE | &quot;service&quot;
PERSONNEL | &quot;personnel&quot;

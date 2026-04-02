package api

import (
	"encoding/json"
)

// Helper functions to convert between internal Addon types and OpenAPI-generated types

// fromIntPtr converts an int pointer to an int
func fromIntPtr(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// toStringSlicePtr converts []string to *[]string
func toStringSlicePtr(s []string) *[]string {
	if len(s) == 0 {
		return nil
	}
	return &s
}

// fromObjectsSlicePtr converts *[]CreateAddonRequestObjects to []string
func fromObjectsSlicePtr(objs *[]CreateAddonRequestObjects) []string {
	if objs == nil {
		return nil
	}
	result := make([]string, len(*objs))
	for i, obj := range *objs {
		result[i] = string(obj)
	}
	return result
}

// toObjectTypeString converts *InvokeAddonRequestObjectType to string
func toObjectTypeString(ot *InvokeAddonRequestObjectType) string {
	if ot == nil {
		return ""
	}
	return string(*ot)
}

// payloadToString converts *map[string]interface{} to JSON string
func payloadToString(p *map[string]any) string {
	if p == nil {
		return "{}"
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// statusToInvokeAddonResponseStatus converts string to InvokeAddonResponseStatus
func statusToInvokeAddonResponseStatus(s string) InvokeAddonResponseStatus {
	return InvokeAddonResponseStatus(s)
}

// fromAddonParametersPtr converts *[]AddonParameter to []AddonParameter
func fromAddonParametersPtr(params *[]AddonParameter) []AddonParameter {
	if params == nil {
		return nil
	}
	return *params
}

// addonToResponse converts internal Addon to OpenAPI AddonResponse
// Returns a zero-value AddonResponse if addon is nil (defensive programming)
func addonToResponse(addon *Addon) AddonResponse {
	if addon == nil {
		return AddonResponse{}
	}
	resp := AddonResponse{
		Id:            addon.ID,
		CreatedAt:     addon.CreatedAt,
		Name:          addon.Name,
		WebhookId:     addon.WebhookID,
		Description:   strPtr(addon.Description),
		Icon:          strPtr(addon.Icon),
		Objects:       toStringSlicePtr(addon.Objects),
		ThreatModelId: addon.ThreatModelID,
	}
	if len(addon.Parameters) > 0 {
		resp.Parameters = &addon.Parameters
	}
	return resp
}

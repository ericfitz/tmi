package api

import (
	"encoding/json"
)

// Helper functions to convert between internal Addon types and OpenAPI-generated types

// toStringSlicePtr converts []string to *[]string
// SEM@9a0b9cccd2069219c4d2a0e0c590f0e517138257: convert a string slice to a pointer, returning nil for empty slices (pure)
func toStringSlicePtr(s []string) *[]string {
	if len(s) == 0 {
		return nil
	}
	return &s
}

// fromObjectsSlicePtr converts *[]CreateAddonRequestObjects to []string
// SEM@9a0b9cccd2069219c4d2a0e0c590f0e517138257: convert a pointer to an addon objects slice to a plain string slice (pure)
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
// SEM@9a0b9cccd2069219c4d2a0e0c590f0e517138257: convert a pointer to an InvokeAddonRequestObjectType to a plain string (pure)
func toObjectTypeString(ot *InvokeAddonRequestObjectType) string {
	if ot == nil {
		return ""
	}
	return string(*ot)
}

// payloadToString converts *map[string]interface{} to JSON string
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: serialize an invocation payload map to a JSON string, returning {} on nil or error (pure)
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
// SEM@9a0b9cccd2069219c4d2a0e0c590f0e517138257: convert a status string to the InvokeAddonResponseStatus type (pure)
func statusToInvokeAddonResponseStatus(s string) InvokeAddonResponseStatus {
	return InvokeAddonResponseStatus(s)
}

// fromAddonParametersPtr converts *[]AddonParameter to []AddonParameter
// SEM@15af4eb93978e65654702a2b47f0ebe20df650dc: dereference a pointer to an AddonParameter slice, returning nil for nil input (pure)
func fromAddonParametersPtr(params *[]AddonParameter) []AddonParameter {
	if params == nil {
		return nil
	}
	return *params
}

// addonToResponse converts internal Addon to OpenAPI AddonResponse
// Returns a zero-value AddonResponse if addon is nil (defensive programming)
// SEM@15af4eb93978e65654702a2b47f0ebe20df650dc: convert an internal Addon model to an AddonResponse API DTO (pure)
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

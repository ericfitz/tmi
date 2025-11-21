package api

import (
	"encoding/json"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Helper functions to convert between internal Addon types and OpenAPI-generated types

// toStringPtr converts a string to a pointer
func toStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// fromStringPtr converts a string pointer to a string
func fromStringPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// toIntPtr converts an int to a pointer
func toIntPtr(i int) *int {
	return &i
}

// fromIntPtr converts an int pointer to an int
func fromIntPtr(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// toUUIDPtr converts openapi_types.UUID to a pointer
func toUUIDPtr(u openapi_types.UUID) *openapi_types.UUID {
	return &u
}

// toStringSlicePtr converts []string to *[]string
func toStringSlicePtr(s []string) *[]string {
	if len(s) == 0 {
		return nil
	}
	return &s
}

// fromStringSlicePtr converts *[]string to []string
func fromStringSlicePtr(s *[]string) []string {
	if s == nil {
		return nil
	}
	return *s
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
func payloadToString(p *map[string]interface{}) string {
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

// statusToInvocationResponseStatus converts string to InvocationResponseStatus
func statusToInvocationResponseStatus(s string) InvocationResponseStatus {
	return InvocationResponseStatus(s)
}

// statusToUpdateResponseStatus converts string to UpdateInvocationStatusResponseStatus
func statusToUpdateResponseStatus(s string) UpdateInvocationStatusResponseStatus {
	return UpdateInvocationStatusResponseStatus(s)
}

// statusFromUpdateRequestStatus converts UpdateInvocationStatusRequestStatus to string
func statusFromUpdateRequestStatus(s UpdateInvocationStatusRequestStatus) string {
	return string(s)
}

// addonToResponse converts internal Addon to OpenAPI AddonResponse
func addonToResponse(addon *Addon) AddonResponse {
	return AddonResponse{
		Id:            addon.ID,
		CreatedAt:     addon.CreatedAt,
		Name:          addon.Name,
		WebhookId:     addon.WebhookID,
		Description:   toStringPtr(addon.Description),
		Icon:          toStringPtr(addon.Icon),
		Objects:       toStringSlicePtr(addon.Objects),
		ThreatModelId: addon.ThreatModelID,
	}
}

// invocationToResponse converts internal AddonInvocation to OpenAPI InvocationResponse
func invocationToResponse(inv *AddonInvocation) InvocationResponse {
	return InvocationResponse{
		Id:              inv.ID,
		AddonId:         inv.AddonID,
		ThreatModelId:   inv.ThreatModelID,
		ObjectType:      toStringPtr(inv.ObjectType),
		ObjectId:        inv.ObjectID,
		InvokedBy:       inv.InvokedBy,
		Payload:         toStringPtr(inv.Payload),
		Status:          statusToInvocationResponseStatus(inv.Status),
		StatusPercent:   inv.StatusPercent,
		StatusMessage:   toStringPtr(inv.StatusMessage),
		CreatedAt:       inv.CreatedAt,
		StatusUpdatedAt: inv.StatusUpdatedAt,
	}
}

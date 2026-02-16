package api

import (
	"encoding/json"

	openapi_types "github.com/oapi-codegen/runtime/types"
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
// Returns a zero-value AddonResponse if addon is nil (defensive programming)
func addonToResponse(addon *Addon) AddonResponse {
	if addon == nil {
		return AddonResponse{}
	}
	return AddonResponse{
		Id:            addon.ID,
		CreatedAt:     addon.CreatedAt,
		Name:          addon.Name,
		WebhookId:     addon.WebhookID,
		Description:   strPtr(addon.Description),
		Icon:          strPtr(addon.Icon),
		Objects:       toStringSlicePtr(addon.Objects),
		ThreatModelId: addon.ThreatModelID,
	}
}

// invocationToResponse converts internal AddonInvocation to OpenAPI InvocationResponse
// Returns a zero-value InvocationResponse if inv is nil (defensive programming)
func invocationToResponse(inv *AddonInvocation) InvocationResponse {
	if inv == nil {
		return InvocationResponse{}
	}
	return InvocationResponse{
		Id:            inv.ID,
		AddonId:       inv.AddonID,
		ThreatModelId: inv.ThreatModelID,
		ObjectType:    strPtr(inv.ObjectType),
		ObjectId:      inv.ObjectID,
		InvokedBy: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "unknown", // TODO: Store provider in AddonInvocation
			ProviderId:    inv.InvokedByID,
			DisplayName:   inv.InvokedByName,
			Email:         openapi_types.Email(inv.InvokedByEmail),
		},
		Payload:         strPtr(inv.Payload),
		Status:          statusToInvocationResponseStatus(inv.Status),
		StatusPercent:   inv.StatusPercent,
		StatusMessage:   strPtr(inv.StatusMessage),
		CreatedAt:       inv.CreatedAt,
		StatusUpdatedAt: inv.StatusUpdatedAt,
	}
}

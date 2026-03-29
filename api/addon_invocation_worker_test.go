package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookDeliveryPayload_MarshalJSON(t *testing.T) {
	addonID := uuid.New()
	threatModelID := uuid.New()
	objectID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)

	userData := json.RawMessage(`{"key":"value"}`)
	data := WebhookDeliveryData{
		AddonID:  &addonID,
		UserData: &userData,
	}
	dataBytes, err := json.Marshal(data)
	require.NoError(t, err)

	payload := WebhookDeliveryPayload{
		EventType:     "addon.invoked",
		ThreatModelID: threatModelID,
		Timestamp:     now,
		ObjectType:    "threat",
		ObjectID:      &objectID,
		Data:          json.RawMessage(dataBytes),
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(payloadBytes, &result)
	require.NoError(t, err)

	assert.Equal(t, "addon.invoked", result["event_type"])
	assert.Equal(t, threatModelID.String(), result["threat_model_id"])
	assert.Equal(t, "threat", result["object_type"])
	assert.Equal(t, objectID.String(), result["object_id"])
	assert.NotNil(t, result["data"])

	// Verify no removed fields are present
	assert.Nil(t, result["invocation_id"])
	assert.Nil(t, result["addon_id"])
	assert.Nil(t, result["callback_url"])

	// Verify data contains addon_id
	dataMap, ok := result["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, addonID.String(), dataMap["addon_id"])
	assert.NotNil(t, dataMap["user_data"])
}

func TestWebhookDeliveryPayload_OptionalFields(t *testing.T) {
	threatModelID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)

	payload := WebhookDeliveryPayload{
		EventType:     "threat_model.updated",
		ThreatModelID: threatModelID,
		Timestamp:     now,
		Data:          json.RawMessage(`{"name":"test"}`),
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(payloadBytes, &result)
	require.NoError(t, err)

	// object_type and object_id should be absent when not set
	_, hasObjectType := result["object_type"]
	_, hasObjectID := result["object_id"]
	assert.False(t, hasObjectType)
	assert.False(t, hasObjectID)
}

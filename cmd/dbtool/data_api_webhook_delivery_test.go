package main

import (
	"net/http"
	"strings"
	"testing"
)

const (
	testSubscriptionID = "11111111-1111-1111-1111-111111111111"
	testWebhookRef     = "webhook:CATS Test Webhook"
)

func webhookDeliveryRefs() RefMap {
	return RefMap{
		testWebhookRef: &SeedResult{Ref: testWebhookRef, Kind: kindWebhook, ID: testSubscriptionID},
	}
}

func webhookDeliveryEntry() SeedEntry {
	return SeedEntry{
		Kind: kindWebhookTestDeliv,
		Data: map[string]any{"webhook_ref": testWebhookRef},
	}
}

// #742: re-seeding must not re-trigger the test delivery. The anchor
// subscription is operator-pinned, and pinned subscriptions reject the
// test-trigger endpoint with 403, so a second `make cats-seed` failed outright.
func TestSeedWebhookTestDelivery_ReusesExistingDelivery(t *testing.T) {
	var postCount int
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCount++
			w.WriteHeader(http.StatusForbidden)
			writeJSON(t, w, map[string]any{"error": "operator-pinned subscription"})
			return
		}
		writeJSON(t, w, map[string]any{"deliveries": []any{
			map[string]any{"id": "delivery-existing", "subscription_id": testSubscriptionID},
		}})
	})

	result, err := c.seedWebhookTestDelivery(webhookDeliveryEntry(), webhookDeliveryRefs())
	if err != nil {
		t.Fatalf("seedWebhookTestDelivery: %v", err)
	}
	if result.ID != "delivery-existing" {
		t.Fatalf("ID = %q, want %q", result.ID, "delivery-existing")
	}
	if postCount != 0 {
		t.Fatalf("test-trigger POSTed %d times, want 0 when a delivery already exists", postCount)
	}
}

func TestSeedWebhookTestDelivery_TriggersWhenNoneExists(t *testing.T) {
	var gotPostPath string
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotPostPath = r.URL.Path
			writeJSON(t, w, map[string]any{"delivery_id": "delivery-new"})
			return
		}
		writeJSON(t, w, map[string]any{"deliveries": []any{}})
	})

	result, err := c.seedWebhookTestDelivery(webhookDeliveryEntry(), webhookDeliveryRefs())
	if err != nil {
		t.Fatalf("seedWebhookTestDelivery: %v", err)
	}
	if result.ID != "delivery-new" {
		t.Fatalf("ID = %q, want %q", result.ID, "delivery-new")
	}
	wantPath := "/admin/webhooks/subscriptions/" + testSubscriptionID + "/test"
	if gotPostPath != wantPath {
		t.Fatalf("POST path %q, want %q", gotPostPath, wantPath)
	}
}

// Without a DB connection the seeder cannot clear the pin, so the 403 must be
// reported with an explanation rather than the bare status the issue showed.
func TestSeedWebhookTestDelivery_PinnedWithoutDBExplainsFailure(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusForbidden)
			writeJSON(t, w, map[string]any{"error": "operator-pinned subscription"})
			return
		}
		writeJSON(t, w, map[string]any{"deliveries": []any{}})
	})

	_, err := c.seedWebhookTestDelivery(webhookDeliveryEntry(), webhookDeliveryRefs())
	if err == nil {
		t.Fatal("expected an error when the subscription is pinned and no DB is available")
	}
	if !strings.Contains(err.Error(), "operator-pinned") {
		t.Fatalf("error %q does not mention the pin", err)
	}
}

func TestFindExistingWebhookDelivery_IgnoresOtherSubscriptions(t *testing.T) {
	var gotQuery string
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("subscription_id")
		writeJSON(t, w, map[string]any{"deliveries": []any{
			map[string]any{"id": "delivery-other", "subscription_id": "22222222-2222-2222-2222-222222222222"},
		}})
	})

	if got := c.findExistingWebhookDelivery(testSubscriptionID); got != "" {
		t.Fatalf("got %q, want empty for a delivery belonging to another subscription", got)
	}
	if gotQuery != testSubscriptionID {
		t.Fatalf("subscription_id query = %q, want %q", gotQuery, testSubscriptionID)
	}
}

func TestFindExistingWebhookDelivery_ErrorStatusReturnsEmpty(t *testing.T) {
	c := newTestAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if got := c.findExistingWebhookDelivery(testSubscriptionID); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

package api

import (
	"sync"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"gorm.io/gorm/schema"
)

// TestUpdateFieldLists_ResolveToSchemaFields guards the Go-field-name lists
// passed to GORM Select: a typo resolves to no field and GORM silently drops
// the column while still reporting RowsAffected == 1 (#878 Oracle review).
func TestUpdateFieldLists_ResolveToSchemaFields(t *testing.T) {
	cases := []struct {
		model  any
		fields []string
	}{
		{&models.WebhookSubscription{}, webhookSubscriptionUpdatableFields},
		{&models.WebhookSubscription{}, []string{"Name", "URL", "Events", "ThreatModelID", "Status", "Challenge", "ChallengesSent"}},
		{&models.Addon{}, addonUpdatableFields},
	}
	for _, tc := range cases {
		s, err := schema.Parse(tc.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("schema.Parse: %v", err)
		}
		for _, f := range tc.fields {
			if s.LookUpField(f) == nil {
				t.Errorf("%s: field %q does not resolve on the GORM schema", s.Name, f)
			}
		}
	}
}

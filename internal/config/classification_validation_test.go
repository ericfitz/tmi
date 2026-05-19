package config

import (
	"strings"
	"testing"
)

func validClass() ConfigClass {
	return ConfigClass{
		Category:   CategoryOperational,
		Secret:     false,
		ValueKind:  ValueKindInline,
		Delivery:   &Delivery{},
		Visibility: VisibilityInternal,
		Mutability: MutabilityHot,
		Consumers:  []Consumer{ConsumerMonolith},
		Required:   false,
	}
}

func TestValidateClassifications_RejectsUnclassified(t *testing.T) {
	s := []MigratableSetting{{Key: "x", Description: "x", Class: ConfigClass{}}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "unclassified") {
		t.Fatalf("want unclassified error, got %v", err)
	}
}

func TestValidateClassifications_BootstrapHasNoDelivery(t *testing.T) {
	c := validClass()
	c.Category = CategoryBootstrap
	c.Delivery = &Delivery{}
	c.Consumers = []Consumer{ConsumerMonolith}
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Fatalf("want bootstrap-delivery error, got %v", err)
	}
}

func TestValidateClassifications_SharedInvariantImpliesStamped(t *testing.T) {
	c := validClass()
	c.Delivery = &Delivery{StampedIntoEnvelope: false, SharedInvariant: true}
	c.Consumers = []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed}
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "SharedInvariant") {
		t.Fatalf("want SharedInvariant-implies-Stamped error, got %v", err)
	}
}

func TestValidateClassifications_SharedInvariantNeedsWorkerConsumer(t *testing.T) {
	c := validClass()
	c.Delivery = &Delivery{StampedIntoEnvelope: true, SharedInvariant: true}
	c.Consumers = []Consumer{ConsumerMonolith}
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("want SharedInvariant-needs-worker error, got %v", err)
	}
}

func TestValidateClassifications_ReferenceImpliesSecret(t *testing.T) {
	c := validClass()
	c.Secret = false
	c.ValueKind = ValueKindReference
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "Reference") {
		t.Fatalf("want Reference-implies-Secret error, got %v", err)
	}
}

func TestValidateClassifications_PublicCannotBeSecret(t *testing.T) {
	c := validClass()
	c.Secret = true
	c.Visibility = VisibilityPublic
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "public") {
		t.Fatalf("want public-cannot-be-secret error, got %v", err)
	}
}

func TestValidateClassifications_NeedsDescriptionAndConsumer(t *testing.T) {
	c := validClass()
	c.Consumers = nil
	s := []MigratableSetting{{Key: "x", Description: "", Class: c}}
	err := ValidateClassifications(s)
	if err == nil {
		t.Fatal("want error for empty description and no consumer")
	}
	if !strings.Contains(err.Error(), "Description") || !strings.Contains(err.Error(), "Consumers") {
		t.Errorf("want both Description and Consumers violations, got %v", err)
	}
}

func TestValidateClassifications_RequiredImpliesBootstrap(t *testing.T) {
	// Required on an operational setting has no enforcement point and is
	// therefore disallowed — the validation suite must reject it.
	c := validClass() // CategoryOperational by default
	c.Required = true
	s := []MigratableSetting{{Key: "x", Description: "x", Class: c}}
	err := ValidateClassifications(s)
	if err == nil || !strings.Contains(err.Error(), "Required") {
		t.Fatalf("want Required-implies-bootstrap error, got %v", err)
	}
}

func TestValidateClassifications_AcceptsValidSet(t *testing.T) {
	good := []MigratableSetting{
		{Key: "database.url", Description: "DB URL", Class: ConfigClass{
			Category: CategoryBootstrap, Visibility: VisibilityInternal,
			Mutability: MutabilityStatic, Consumers: []Consumer{ConsumerMonolith},
			Required: true,
		}},
		{Key: "embedding.model", Description: "Embedding model", Class: ConfigClass{
			Category: CategoryOperational, Visibility: VisibilityAdminOnly,
			Mutability: MutabilityHot,
			Delivery:   &Delivery{StampedIntoEnvelope: true, SharedInvariant: true},
			Consumers:  []Consumer{ConsumerMonolith, ConsumerWorkerChunkEmbed},
		}},
	}
	if err := ValidateClassifications(good); err != nil {
		t.Fatalf("valid set rejected: %v", err)
	}
}

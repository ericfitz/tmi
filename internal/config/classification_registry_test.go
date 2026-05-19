package config

import "testing"

func TestClassificationFor_KnownBootstrapKey(t *testing.T) {
	c := classificationFor("database.url")
	if c.Category != CategoryBootstrap {
		t.Errorf("database.url Category = %v, want CategoryBootstrap", c.Category)
	}
}

func TestClassificationFor_KnownOperationalKey(t *testing.T) {
	c := classificationFor("websocket.inactivity_timeout_seconds")
	if c.Category != CategoryOperational {
		t.Errorf("websocket.inactivity_timeout_seconds Category = %v, want CategoryOperational", c.Category)
	}
}

func TestClassificationFor_SharedEmbeddingKey(t *testing.T) {
	c := classificationFor("timmy.text_embedding_model")
	if c.Delivery == nil || !c.Delivery.SharedInvariant {
		t.Errorf("timmy.text_embedding_model should be a SharedInvariant setting, got %+v", c.Delivery)
	}
}

func TestClassificationFor_ProviderPrefixKey(t *testing.T) {
	c := classificationFor("auth.oauth.providers.google.client_secret")
	if !c.Secret {
		t.Error("oauth provider client_secret should be classified Secret")
	}
}

func TestClassificationFor_UnknownKeyIsUnclassified(t *testing.T) {
	c := classificationFor("totally.unknown.key")
	if c.Category != CategoryUnclassified {
		t.Errorf("unknown key Category = %v, want CategoryUnclassified", c.Category)
	}
}

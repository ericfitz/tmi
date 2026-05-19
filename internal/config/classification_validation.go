package config

import (
	"fmt"
	"strings"
)

// ValidateClassifications checks that every setting carries a complete and
// internally consistent ConfigClass. It returns a single error listing every
// violation found. This is the mechanism that makes the classification model
// self-enforcing: a misclassified setting fails the build.
func ValidateClassifications(settings []MigratableSetting) error {
	var problems []string
	add := func(key, msg string) {
		problems = append(problems, fmt.Sprintf("%s: %s", key, msg))
	}

	for _, s := range settings {
		c := s.Class

		if c.Category == CategoryUnclassified {
			add(s.Key, "unclassified — Category is the zero value")
			continue
		}
		if s.Description == "" {
			add(s.Key, "empty Description")
		}
		if len(c.Consumers) == 0 {
			add(s.Key, "no Consumers declared")
		}

		switch c.Category {
		case CategoryBootstrap:
			if c.Delivery != nil {
				add(s.Key, "bootstrap setting must not carry a Delivery")
			}
		case CategoryOperational:
			if c.Delivery == nil {
				add(s.Key, "operational setting must carry a Delivery")
				continue
			}
			if c.Delivery.SharedInvariant && !c.Delivery.StampedIntoEnvelope {
				add(s.Key, "SharedInvariant requires StampedIntoEnvelope")
			}
			if c.Delivery.SharedInvariant && !hasWorkerConsumer(c.Consumers) {
				add(s.Key, "SharedInvariant requires at least one worker Consumer")
			}
			if c.Delivery.SharedInvariant && !hasConsumer(c.Consumers, ConsumerMonolith) {
				add(s.Key, "SharedInvariant requires the monolith as a Consumer")
			}
		default:
			add(s.Key, fmt.Sprintf("unknown Category value %d — update ValidateClassifications", c.Category))
		}

		if c.ValueKind == ValueKindReference && !c.Secret {
			add(s.Key, "ValueKindReference is only valid on a Secret setting")
		}
		if c.Visibility == VisibilityPublic && c.Secret {
			add(s.Key, "a public setting must not be a secret")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("config classification invalid:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// hasConsumer reports whether want is present in cs.
func hasConsumer(cs []Consumer, want Consumer) bool {
	for _, c := range cs {
		if c == want {
			return true
		}
	}
	return false
}

// hasWorkerConsumer reports whether cs includes any worker consumer.
// Update this if a new ConsumerWorker* constant is added.
func hasWorkerConsumer(cs []Consumer) bool {
	return hasConsumer(cs, ConsumerWorkerExtractor) || hasConsumer(cs, ConsumerWorkerChunkEmbed)
}

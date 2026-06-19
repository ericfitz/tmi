package config

// Category answers: where does the value come from at rest?
// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: enum classifying where a config value originates at rest (pure)
type Category int

const (
	// CategoryUnclassified is the zero value. A setting with this category
	// fails the validation suite — it forces every setting to be classified.
	CategoryUnclassified Category = iota
	// CategoryBootstrap settings are loaded from file/env only, never the DB,
	// and consumed at startup before the settings service exists.
	CategoryBootstrap
	// CategoryOperational settings are DB-backed and runtime-editable.
	CategoryOperational
)

// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: convert a Category to its canonical string name (pure)
func (c Category) String() string {
	switch c {
	case CategoryBootstrap:
		return "bootstrap"
	case CategoryOperational:
		return "operational"
	default:
		return "unclassified"
	}
}

// ValueKind answers: is the stored value the secret itself, or a pointer to it?
//
// The registry records the DEFAULT (inline) kind of a setting. A secret VALUE
// may still be a vault://, env://, or file:// reference at runtime: the same
// key is typically inline in dev and a reference in prod. Such references are
// dereferenced at startup, per value, by ResolveSecretValue (see
// secret_reference.go) — IsSecretReference inspects the value's scheme prefix,
// so no registry change is needed to support a reference value.
//
// ValueKindReference in the REGISTRY is therefore reserved for a key that is
// ALWAYS a reference regardless of deployment; today no key is classified that
// way, so the "ValueKindReference => Secret" validation rule holds vacuously.
// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: enum indicating whether a config field holds an inline value or a secret reference (pure)
type ValueKind int

const (
	// ValueKindInline means the field holds the actual value. This is the
	// zero value and a safe default for non-secret settings. A secret field
	// classified inline may still carry a vault://, env://, or file://
	// reference value, resolved at load time by ResolveSecretValue.
	ValueKindInline ValueKind = iota
	// ValueKindReference means the field ALWAYS holds a locator (vault://...,
	// a file path, an env-var name) dereferenced at use time, in every
	// deployment. Only valid when Secret. Per-value references on an
	// otherwise-inline key do NOT require this — see the type doc above.
	ValueKindReference
)

// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: convert a ValueKind to its canonical string name (pure)
func (v ValueKind) String() string {
	if v == ValueKindReference {
		return "reference"
	}
	return "inline"
}

// Visibility answers: who may read this setting through the API?
// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: enum classifying which API audience may read a config setting (pure)
type Visibility int

const (
	// VisibilityInternal: server-side only, never in any API response.
	VisibilityInternal Visibility = iota
	// VisibilityAdminOnly: visible to admins via /admin config endpoints.
	VisibilityAdminOnly
	// VisibilityPublic: exposed on the unauthenticated /config endpoint.
	VisibilityPublic
)

// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: convert a Visibility to its canonical string name (pure)
func (v Visibility) String() string {
	switch v {
	case VisibilityAdminOnly:
		return "admin-only"
	case VisibilityPublic:
		return "public"
	default:
		return "internal"
	}
}

// Mutability answers: can it change after startup?
// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: enum indicating whether a config setting requires restart to change (pure)
type Mutability int

const (
	// MutabilityStatic: read once at boot; a change needs a restart.
	MutabilityStatic Mutability = iota
	// MutabilityHot: re-read at use time; a runtime edit takes effect at once.
	MutabilityHot
)

// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: convert a Mutability to its canonical string name (pure)
func (m Mutability) String() string {
	if m == MutabilityHot {
		return "hot"
	}
	return "static"
}

// Consumer is a closed enum of the processes that read configuration.
// Add a value here when a new component type is introduced.
// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: enum of process types that read configuration (pure)
type Consumer int

const (
	// ConsumerMonolith is the primary TMI server process.
	ConsumerMonolith Consumer = iota
	// ConsumerTMIUX is the tmi-ux frontend client.
	ConsumerTMIUX
	// ConsumerWorkerExtractor is the extractor worker process.
	ConsumerWorkerExtractor
	// ConsumerWorkerChunkEmbed is the chunk-embed worker process.
	ConsumerWorkerChunkEmbed
)

// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: convert a Consumer to its canonical process-name string (pure)
func (c Consumer) String() string {
	switch c {
	case ConsumerTMIUX:
		return "tmi-ux"
	case ConsumerWorkerExtractor:
		return "worker:extractor"
	case ConsumerWorkerChunkEmbed:
		return "worker:chunk-embed"
	default:
		return "monolith"
	}
}

// Delivery describes how an operational setting reaches a process that cannot
// ask the monolith over HTTP. It is nil on bootstrap settings.
// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: struct describing how an operational setting is delivered to non-monolith processes (pure)
type Delivery struct {
	// StampedIntoEnvelope: the monolith copies this into job envelopes.
	StampedIntoEnvelope bool
	// SharedInvariant: the monolith ALSO consumes this; ingest and the
	// monolith must agree. The validation suite enforces that SharedInvariant
	// implies StampedIntoEnvelope.
	SharedInvariant bool
}

// ConfigClass is the complete classification of one configuration item.
// SEM@84a87a09a2929e0ab55a7f5b5a619f296148d6a1: complete classification metadata for one configuration setting (pure)
type ConfigClass struct {
	Category   Category
	Secret     bool
	ValueKind  ValueKind
	Delivery   *Delivery // nil for CategoryBootstrap
	Visibility Visibility
	Mutability Mutability
	Consumers  []Consumer
	Required   bool
}

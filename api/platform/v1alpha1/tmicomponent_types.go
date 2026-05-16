package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EgressPosture controls the NetworkPolicy the controller renders.
// +kubebuilder:validation:Enum=none;fetch-controlled;allowlist
type EgressPosture string

const (
	// EgressNone denies all egress except NATS. Used by parse-only workers.
	EgressNone EgressPosture = "none"
	// EgressFetchControlled allows guarded outbound fetch. RESERVED — not
	// exercised until the code extractor (a later issue).
	EgressFetchControlled EgressPosture = "fetch-controlled"
	// EgressAllowlist allows egress to specific hosts only.
	EgressAllowlist EgressPosture = "allowlist"
)

// InputMode declares how a component receives job input.
// +kubebuilder:validation:Enum=content-ref;source-locator
type InputMode string

const (
	// InputContentRef means the job carries an object_ref to bytes the
	// monolith already placed in the JetStream Object Store.
	InputContentRef InputMode = "content-ref"
	// InputSourceLocator means the job carries a URL the worker fetches.
	// RESERVED — not exercised until the code extractor.
	InputSourceLocator InputMode = "source-locator"
)

// SecretRef points to a key in a Kubernetes Secret. Secrets are NEVER
// inlined in a TMIComponent — only referenced.
type SecretRef struct {
	// Name is the logical name the worker uses to find this secret.
	Name string `json:"name"`
	// SecretName is the Kubernetes Secret object name.
	SecretName string `json:"secretName"`
	// SecretKey is the key within that Secret.
	SecretKey string `json:"secretKey"`
}

// AllowlistEgress lists hosts a component with egress: allowlist may reach.
type AllowlistEgress struct {
	// Hosts is the set of DNS names allowed for outbound traffic.
	// +optional
	Hosts []string `json:"hosts,omitempty"`
}

// ScalingSpec configures the KEDA ScaledObject.
type ScalingSpec struct {
	// +kubebuilder:validation:Minimum=0
	MinReplicas int32 `json:"minReplicas"`
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`
	// QueueDepthTarget is the JetStream pending-message count per replica
	// KEDA scales toward.
	// +kubebuilder:validation:Minimum=1
	QueueDepthTarget int32 `json:"queueDepthTarget"`
}

// ScratchVolume requests a capped, ephemeral emptyDir mounted writable.
type ScratchVolume struct {
	// MountPath is where the emptyDir is mounted in the worker container.
	MountPath string `json:"mountPath"`
	// SizeLimit caps the emptyDir (e.g. "512Mi"). Serialized as a string
	// in YAML; validated as a Kubernetes quantity at admission time.
	SizeLimit resource.Quantity `json:"sizeLimit"`
}

// TMIComponentSpec is the desired state of a component type.
type TMIComponentSpec struct {
	// Image is the worker container image.
	Image string `json:"image"`
	// JobSubjects are the JetStream subjects this component consumes.
	// +kubebuilder:validation:MinItems=1
	JobSubjects []string `json:"jobSubjects"`
	// InputMode declares how the component receives job input.
	InputMode InputMode `json:"inputMode"`
	// Egress is the network posture; drives the rendered NetworkPolicy.
	Egress EgressPosture `json:"egress"`
	// Allowlist is required when Egress is "allowlist", ignored otherwise.
	// +optional
	Allowlist *AllowlistEgress `json:"allowlist,omitempty"`
	// Config holds non-secret component-local config values, passed to the
	// worker as environment variables.
	// +optional
	Config map[string]string `json:"config,omitempty"`
	// SecretRefs reference Kubernetes Secrets wired into the worker pod.
	// +optional
	SecretRefs []SecretRef `json:"secretRefs,omitempty"`
	// Resources sets the worker pod CPU/memory limits (the cgroup caps).
	Resources corev1.ResourceRequirements `json:"resources"`
	// ScratchVolume optionally requests a writable emptyDir.
	// +optional
	ScratchVolume *ScratchVolume `json:"scratchVolume,omitempty"`
	// Scaling configures KEDA autoscaling.
	Scaling ScalingSpec `json:"scaling"`
}

// TMIComponentStatus is the observed state.
type TMIComponentStatus struct {
	// Conditions follows the standard Kubernetes condition pattern.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Namespaced,shortName=tmicomp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Egress",type=string,JSONPath=`.spec.egress`
// +kubebuilder:printcolumn:name="Input",type=string,JSONPath=`.spec.inputMode`

// TMIComponent declares a TMI Component Platform worker type.
type TMIComponent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TMIComponentSpec   `json:"spec,omitempty"`
	Status TMIComponentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TMIComponentList is a list of TMIComponent.
type TMIComponentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TMIComponent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TMIComponent{}, &TMIComponentList{})
}

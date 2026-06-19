package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EgressPosture controls the NetworkPolicy the controller renders.
// +kubebuilder:validation:Enum=none;fetch-controlled;allowlist
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: enum type controlling the network egress policy rendered for a worker component (pure)
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
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: enum type declaring how a worker component receives job input (pure)
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
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: data container referencing a key in a Kubernetes Secret by name without inlining secrets (pure)
type SecretRef struct {
	// Name is the logical name the worker uses to find this secret.
	Name string `json:"name"`
	// SecretName is the Kubernetes Secret object name.
	SecretName string `json:"secretName"`
	// SecretKey is the key within that Secret.
	SecretKey string `json:"secretKey"`
}

// AllowlistEgress declares the server-side-enforceable egress targets for a
// component with egress: allowlist. At least one of CIDRs, ClusterPeers, or
// OpenInternet must be set (enforced by ValidateComponent). The cloud metadata
// IP (169.254.169.254) is never reachable regardless of what is declared here.
// SEM@63e2aad01818e6abee8652287d395a8b4f205986: data container declaring allowed egress targets for a component with allowlist network posture (pure)
type AllowlistEgress struct {
	// CIDRs are stable destination ranges rendered as NetworkPolicy ipBlock
	// egress rules. Use for an in-cluster VM, a cloud private-endpoint subnet,
	// or a known API VIP. Validation rejects 0.0.0.0/0 and any range covering
	// the metadata IP.
	// +optional
	CIDRs []string `json:"cidrs,omitempty"`
	// ClusterPeers are in-cluster destinations rendered as namespace/pod-selector
	// egress rules (e.g. an in-cluster embedder Service's pods).
	// +optional
	ClusterPeers []ClusterPeer `json:"clusterPeers,omitempty"`
	// OpenInternet, when true, renders broad egress (0.0.0.0/0 minus RFC1918 and
	// minus the metadata IP) on the declared ports. Host-exactness is DELEGATED
	// to operator infrastructure (cloud egress firewall / managed-CNI FQDN
	// policy). The explicit escape hatch for a target that cannot be reduced to
	// a CIDR (public-SaaS model APIs).
	// +optional
	OpenInternet bool `json:"openInternet,omitempty"`
	// Ports the egress rules apply to. Defaults to TCP/443 when empty.
	// +optional
	Ports []int32 `json:"ports,omitempty"`
}

// ClusterPeer selects an in-cluster egress destination by namespace and pod
// labels. At least one of NamespaceSelector / PodSelector must be set.
// SEM@63e2aad01818e6abee8652287d395a8b4f205986: data container selecting an in-cluster egress destination by namespace and pod labels (pure)
type ClusterPeer struct {
	// NamespaceSelector matches destination namespaces by label. When empty,
	// the rule is not namespace-scoped (matches pods in any namespace by the
	// PodSelector).
	// +optional
	NamespaceSelector map[string]string `json:"namespaceSelector,omitempty"`
	// PodSelector matches destination pods by label.
	// +optional
	PodSelector map[string]string `json:"podSelector,omitempty"`
	// Ports the rule applies to. Defaults to TCP/443 when empty.
	// +optional
	Ports []int32 `json:"ports,omitempty"`
}

// ScalingSpec configures the KEDA ScaledObject.
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: data container configuring KEDA autoscaling bounds and queue-depth target for a component (pure)
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
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: data container declaring a capped ephemeral emptyDir volume for a worker pod (pure)
type ScratchVolume struct {
	// MountPath is where the emptyDir is mounted in the worker container.
	MountPath string `json:"mountPath"`
	// SizeLimit caps the emptyDir (e.g. "512Mi"). Serialized as a string
	// in YAML; validated as a Kubernetes quantity at admission time.
	SizeLimit resource.Quantity `json:"sizeLimit"`
}

// TMIComponentSpec is the desired state of a component type.
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: desired-state spec for a TMI worker component type including image, scaling, and network policy (pure)
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
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: observed-state status for a TMI worker component with Kubernetes conditions (pure)
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
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: Kubernetes custom resource declaring a TMI Component Platform worker type (pure)
type TMIComponent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TMIComponentSpec   `json:"spec,omitempty"`
	Status TMIComponentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TMIComponentList is a list of TMIComponent.
// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: list container for TMIComponent custom resources (pure)
type TMIComponentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TMIComponent `json:"items"`
}

// SEM@903a46db5bf7674feed65c5c638d24d77f8bf47c: register TMIComponent and TMIComponentList types with the controller-runtime scheme (mutates shared state)
func init() {
	SchemeBuilder.Register(&TMIComponent{}, &TMIComponentList{})
}

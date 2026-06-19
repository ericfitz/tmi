package controller

import (
	"sort"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// nonRootUID / nonRootGID are the high UID/GID worker containers run as.
// 65532 is the conventional "nonroot" id baked into Chainguard/distroless
// images; pinning the GID (not just the UID) removes the implicit reliance
// on the worker image's primary group.
const (
	nonRootUID int64 = 65532
	nonRootGID int64 = 65532
)

// RenderDeployment builds the worker Deployment for a TMIComponent.
// Pod hardening (readOnlyRootFilesystem, runAsNonRoot, all caps dropped,
// RuntimeDefault seccomp) is applied unconditionally — it is a hard
// platform invariant, not a per-component option.
// SEM@033363404fa8d485d9d46c5454abf63fe8bfc1e5: build a hardened Kubernetes Deployment for a TMIComponent worker with non-root security context (pure)
func RenderDeployment(c *platformv1alpha1.TMIComponent) *appsv1.Deployment {
	labels := componentPodLabels(c)

	env := configEnv(c)
	env = append(env, secretEnv(c)...)

	ctr := corev1.Container{
		Name:      "worker",
		Image:     c.Spec.Image,
		Env:       env,
		Resources: c.Spec.Resources,
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem:   boolPtr(true),
			AllowPrivilegeEscalation: boolPtr(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	// FsGroup makes the kubelet chown/ownership-fix mounted volumes to this
	// GID. For a worker whose only writable path is the scratch emptyDir,
	// this guarantees the capped scratch volume is group-writable by the
	// non-root process regardless of the image's primary GID.
	pod := corev1.PodSpec{
		Containers: []corev1.Container{ctr},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   boolPtr(true),
			RunAsUser:      int64Ptr(nonRootUID),
			RunAsGroup:     int64Ptr(nonRootGID),
			FSGroup:        int64Ptr(nonRootGID),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}

	if c.Spec.ScratchVolume != nil {
		// SizeLimit is already a resource.Quantity (validated at admission
		// time by the CRD schema), so no parsing is needed here.
		sizeLimit := c.Spec.ScratchVolume.SizeLimit
		pod.Volumes = []corev1.Volume{{
			Name: "scratch",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &sizeLimit},
			},
		}}
		pod.Containers[0].VolumeMounts = []corev1.VolumeMount{{
			Name: "scratch", MountPath: c.Spec.ScratchVolume.MountPath,
		}}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: c.Name, Namespace: c.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       pod,
			},
		},
	}
}

// configEnv turns spec.config into sorted env vars (sorted for deterministic
// output so reconcile does not thrash on map iteration order).
// SEM@40b1a824a6d8de39df0e2a4d0a1372a4d53c56ad: convert a component's config map to sorted Kubernetes EnvVar literals (pure)
func configEnv(c *platformv1alpha1.TMIComponent) []corev1.EnvVar {
	keys := make([]string, 0, len(c.Spec.Config))
	for k := range c.Spec.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		env = append(env, corev1.EnvVar{Name: k, Value: c.Spec.Config[k]})
	}
	return env
}

// secretEnv turns secretRefs into env vars sourced from K8s Secrets.
// SEM@40b1a824a6d8de39df0e2a4d0a1372a4d53c56ad: convert a component's secret references to Kubernetes SecretKeyRef env vars (pure)
func secretEnv(c *platformv1alpha1.TMIComponent) []corev1.EnvVar {
	env := make([]corev1.EnvVar, 0, len(c.Spec.SecretRefs))
	for _, ref := range c.Spec.SecretRefs {
		env = append(env, corev1.EnvVar{
			Name: ref.Name,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ref.SecretName},
					Key:                  ref.SecretKey,
				},
			},
		})
	}
	return env
}

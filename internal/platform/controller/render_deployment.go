package controller

import (
	"sort"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// nonRootUID is the high UID worker containers run as.
const nonRootUID int64 = 65532

// RenderDeployment builds the worker Deployment for a TMIComponent.
// Pod hardening (readOnlyRootFilesystem, runAsNonRoot, all caps dropped,
// RuntimeDefault seccomp) is applied unconditionally — it is a hard
// platform invariant, not a per-component option.
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

	pod := corev1.PodSpec{
		Containers: []corev1.Container{ctr},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   boolPtr(true),
			RunAsUser:      int64Ptr(nonRootUID),
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

func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }

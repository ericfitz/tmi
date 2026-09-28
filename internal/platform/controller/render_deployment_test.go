package controller

import (
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func deployComp() *platformv1alpha1.TMIComponent {
	return &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-extractor", Namespace: "tmi-platform"},
		Spec: platformv1alpha1.TMIComponentSpec{
			Image:     "tmi/extractor:dev",
			InputMode: platformv1alpha1.InputContentRef,
			Egress:    platformv1alpha1.EgressNone,
			Config:    map[string]string{"WALL_CLOCK_SECONDS": "30"},
			SecretRefs: []platformv1alpha1.SecretRef{
				{Name: "embed", SecretName: "embed-creds", SecretKey: "api-key"},
			},
		},
	}
}

func TestRenderDeployment_HardensPodSecurity(t *testing.T) {
	d := RenderDeployment(deployComp())
	pod := d.Spec.Template.Spec
	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Fatal("pod must runAsNonRoot")
	}
	if pod.SecurityContext.RunAsUser == nil || *pod.SecurityContext.RunAsUser == 0 {
		t.Fatal("pod must run as a non-zero UID")
	}
	if pod.SecurityContext.RunAsGroup == nil || *pod.SecurityContext.RunAsGroup == 0 {
		t.Fatal("pod must run as a non-zero GID (no implicit reliance on the image's primary group)")
	}
	if pod.SecurityContext.FSGroup == nil || *pod.SecurityContext.FSGroup == 0 {
		t.Fatal("pod must set a non-zero fsGroup so the scratch emptyDir is group-writable")
	}
	if pod.SecurityContext.SeccompProfile == nil ||
		pod.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatal("pod must use the RuntimeDefault seccomp profile")
	}
	ctr := pod.Containers[0]
	if ctr.SecurityContext == nil {
		t.Fatal("container securityContext missing")
	}
	if ctr.SecurityContext.ReadOnlyRootFilesystem == nil || !*ctr.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("container must have readOnlyRootFilesystem=true (hard invariant)")
	}
	if ctr.SecurityContext.AllowPrivilegeEscalation == nil || *ctr.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("container must have allowPrivilegeEscalation=false")
	}
	if ctr.SecurityContext.Capabilities == nil || len(ctr.SecurityContext.Capabilities.Drop) == 0 {
		t.Fatal("container must drop all capabilities")
	}
}

func TestRenderDeployment_ConfigBecomesEnvVars(t *testing.T) {
	d := RenderDeployment(deployComp())
	ctr := d.Spec.Template.Spec.Containers[0]
	var found bool
	for _, e := range ctr.Env {
		if e.Name == "WALL_CLOCK_SECONDS" && e.Value == "30" {
			found = true
		}
	}
	if !found {
		t.Fatal("spec.config entries must become container env vars")
	}
}

func TestRenderDeployment_SecretRefBecomesEnvFromSecret(t *testing.T) {
	d := RenderDeployment(deployComp())
	ctr := d.Spec.Template.Spec.Containers[0]
	var found bool
	for _, e := range ctr.Env {
		if e.Name == "embed" && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			if e.ValueFrom.SecretKeyRef.Name == "embed-creds" &&
				e.ValueFrom.SecretKeyRef.Key == "api-key" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("secretRefs must become env vars sourced from the named Secret")
	}
}

func TestRenderDeployment_ScratchVolumeWhenRequested(t *testing.T) {
	c := deployComp()
	c.Spec.ScratchVolume = &platformv1alpha1.ScratchVolume{MountPath: "/scratch", SizeLimit: resource.MustParse("256Mi")}
	d := RenderDeployment(c)
	pod := d.Spec.Template.Spec
	var scratch *corev1.Volume
	for i := range pod.Volumes {
		if pod.Volumes[i].Name == "scratch" {
			scratch = &pod.Volumes[i]
		}
	}
	if scratch == nil || scratch.EmptyDir == nil {
		t.Fatal("scratchVolume must render an emptyDir volume named scratch")
	}
	if scratch.EmptyDir.SizeLimit == nil {
		t.Fatal("scratch emptyDir must be size-capped")
	}
	if _, ok := volumeMountByPath(pod.Containers[0], "/scratch"); !ok {
		t.Fatal("scratch volume must be mounted at the requested path")
	}
}

func volumeMountByPath(c corev1.Container, path string) (corev1.VolumeMount, bool) {
	for _, m := range c.VolumeMounts {
		if m.MountPath == path {
			return m, true
		}
	}
	return corev1.VolumeMount{}, false
}

func TestRenderDeployment_NATSClientTLS(t *testing.T) {
	d := RenderDeployment(deployComp())
	if d.Annotations["reloader.stakater.com/auto"] != "true" {
		t.Fatal("worker Deployment must carry reloader.stakater.com/auto=true so a renewed cert Secret rolls the pods")
	}
	pod := d.Spec.Template.Spec
	var vol *corev1.Volume
	for i := range pod.Volumes {
		if pod.Volumes[i].Name == "nats-client-tls" {
			vol = &pod.Volumes[i]
		}
	}
	if vol == nil || vol.Secret == nil {
		t.Fatal("nats-client-tls Secret volume missing")
	}
	if vol.Secret.SecretName != "nats-client-extractor" {
		t.Fatalf("secret name = %q, want nats-client-extractor (component tmi-extractor)", vol.Secret.SecretName)
	}
	if vol.Secret.Optional == nil || !*vol.Secret.Optional {
		t.Fatal("volume must be optional so a component that never dials NATS still schedules")
	}
	m, ok := volumeMountByPath(pod.Containers[0], "/etc/tmi-nats-tls")
	if !ok || !m.ReadOnly || m.Name != "nats-client-tls" {
		t.Fatalf("client cert must be mounted read-only at /etc/tmi-nats-tls, got %+v", m)
	}
	want := map[string]string{
		"TMI_NATS_TLS_CA_FILE":   "/etc/tmi-nats-tls/ca.crt",
		"TMI_NATS_TLS_CERT_FILE": "/etc/tmi-nats-tls/tls.crt",
		"TMI_NATS_TLS_KEY_FILE":  "/etc/tmi-nats-tls/tls.key",
	}
	for _, e := range pod.Containers[0].Env {
		if v, ok := want[e.Name]; ok && v == e.Value {
			delete(want, e.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing NATS TLS env vars: %v", want)
	}
}

func TestRenderDeployment_ScratchAndTLSVolumesCoexist(t *testing.T) {
	c := deployComp()
	c.Spec.ScratchVolume = &platformv1alpha1.ScratchVolume{MountPath: "/scratch", SizeLimit: resource.MustParse("256Mi")}
	pod := RenderDeployment(c).Spec.Template.Spec
	if len(pod.Volumes) != 2 {
		t.Fatalf("want scratch + nats-client-tls volumes, got %d", len(pod.Volumes))
	}
}

// Command component-controller runs the TMI Component Platform controller.
package main

import (
	"fmt"
	"os"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/ericfitz/tmi/internal/platform/controller"
	"github.com/ericfitz/tmi/internal/slogging"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: entry point for the component-controller binary; delegates to run
func main() {
	if err := run(); err != nil {
		slogging.Get().Error("component-controller: %v", err)
		os.Exit(1)
	}
}

// run is the real entry point. Separating it from main lets deferred cleanup
// (the NATS provisioner connection) execute before the process exits, instead
// of being skipped by an os.Exit in main.
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: register CRD schemes, wire a NATS provisioner, and start the Kubernetes controller manager
func run() error {
	logger := slogging.Get()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return fmt.Errorf("add client-go scheme: %w", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("add platform scheme: %w", err)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	reconciler := &controller.TMIComponentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}

	// Provision the JetStream stream + durable consumer each component's KEDA
	// ScaledObject watches. KEDA scales a worker from zero by reading that
	// consumer's pending depth, so it must exist before any worker pod runs.
	// Out-of-cluster (current e2e flow) TMI_NATS_URL is the host port-forward
	// (nats://127.0.0.1:4222); in-cluster it is the service DNS. If unset,
	// provisioning is disabled and scale-from-zero will not work.
	if natsURL := os.Getenv("TMI_NATS_URL"); natsURL != "" {
		prov, err := controller.NewNATSProvisioner(natsURL)
		if err != nil {
			return fmt.Errorf("nats provisioner: %w", err)
		}
		defer prov.Close()
		reconciler.Streams = prov
		logger.Info("JetStream provisioning enabled, nats=%s", natsURL)
	} else {
		logger.Warn("TMI_NATS_URL unset; JetStream stream/consumer provisioning disabled (workers will not scale from zero)")
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup reconciler: %w", err)
	}

	logger.Info("starting TMI Component Platform controller")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("manager exited: %w", err)
	}
	return nil
}

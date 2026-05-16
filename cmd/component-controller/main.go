// Command component-controller runs the TMI Component Platform controller.
package main

import (
	"os"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/ericfitz/tmi/internal/platform/controller"
	"github.com/ericfitz/tmi/internal/slogging"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	logger := slogging.Get()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		logger.Error("add client-go scheme: %v", err)
		os.Exit(1)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		logger.Error("add platform scheme: %v", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme})
	if err != nil {
		logger.Error("create manager: %v", err)
		os.Exit(1)
	}

	reconciler := &controller.TMIComponentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		logger.Error("setup reconciler: %v", err)
		os.Exit(1)
	}

	logger.Info("starting TMI Component Platform controller")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error("manager exited: %v", err)
		os.Exit(1)
	}
}

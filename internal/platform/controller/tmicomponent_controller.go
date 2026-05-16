package controller

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TMIComponentReconciler reconciles a TMIComponent into its child objects:
// a Deployment, a NetworkPolicy, and a KEDA ScaledObject. JetStream stream
// creation is handled at component startup against a live NATS and is not
// part of object reconciliation.
type TMIComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is the controller-runtime entrypoint.
func (r *TMIComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.ReconcileComponent(ctx, req.NamespacedName)
}

// ReconcileComponent renders and applies the child objects for one component.
// Split out from Reconcile so tests can drive it directly.
func (r *TMIComponentReconciler) ReconcileComponent(ctx context.Context, key types.NamespacedName) (ctrl.Result, error) {
	var comp platformv1alpha1.TMIComponent
	if err := r.Get(ctx, key, &comp); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil // deleted; owner refs garbage-collect children
		}
		return ctrl.Result{}, err
	}

	if err := ValidateComponent(&comp); err != nil {
		return ctrl.Result{}, fmt.Errorf("invalid TMIComponent %s: %w", key, err)
	}

	dep := RenderDeployment(&comp)
	if err := controllerutil.SetControllerReference(&comp, dep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.apply(ctx, dep); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply Deployment: %w", err)
	}

	np := RenderNetworkPolicy(&comp)
	if err := controllerutil.SetControllerReference(&comp, np, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.apply(ctx, np); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply NetworkPolicy: %w", err)
	}

	so := RenderScaledObject(&comp)
	if err := controllerutil.SetControllerReference(&comp, so, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.apply(ctx, so); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply ScaledObject: %w", err)
	}

	return ctrl.Result{}, nil
}

// apply creates the object, or updates it in place if it already exists.
// On update it first fetches the live object to capture its resourceVersion
// (required by the API server for optimistic concurrency), then carries that
// resourceVersion onto the freshly-rendered object before the Update call.
func (r *TMIComponentReconciler) apply(ctx context.Context, obj client.Object) error {
	err := r.Create(ctx, obj)
	if err == nil {
		return nil
	}
	if !errors.IsAlreadyExists(err) {
		return err
	}
	// Object exists: fetch the live copy to obtain its resourceVersion,
	// then update the rendered object in place.
	existing, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return fmt.Errorf("rendered object %T does not implement client.Object", obj)
	}
	key := client.ObjectKeyFromObject(obj)
	if err := r.Get(ctx, key, existing); err != nil {
		return err
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// SetupWithManager registers the reconciler and its owned child types.
func (r *TMIComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Owns the typed children so the controller re-reconciles on child drift.
	// The KEDA ScaledObject is unstructured and not watched here; drift
	// correction for it is tracked as a follow-up.
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.TMIComponent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Complete(r)
}

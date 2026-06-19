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
// a Deployment, a NetworkPolicy, a KEDA ScaledObject, and (when Streams is
// set) the JetStream stream + durable consumer the ScaledObject watches.
// Pre-creating the stream and consumer is required for KEDA scale-from-zero:
// KEDA reads the consumer's pending depth to decide when to scale the worker
// up from zero, so the consumer must exist before any worker pod runs.
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: Kubernetes reconciler that converges a TMIComponent into its Deployment, NetworkPolicy, and ScaledObject children
type TMIComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Streams provisions the JetStream stream and durable consumer for each
	// component. It is optional: envtest unit tests have no NATS and leave it
	// nil, which skips provisioning.
	Streams StreamProvisioner
}

// Reconcile is the controller-runtime entrypoint.
// SEM@50b942b21c528f6a4405c3ce2dccedfdd379012a: controller-runtime entrypoint; delegates to ReconcileComponent
func (r *TMIComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.ReconcileComponent(ctx, req.NamespacedName)
}

// ReconcileComponent renders and applies the child objects for one component.
// Split out from Reconcile so tests can drive it directly.
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: render and apply all child objects for a TMIComponent, provisioning JetStream if configured
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

	// Pre-create the JetStream stream and durable consumer the ScaledObject
	// above watches. Without this, KEDA cannot observe queue depth on a
	// consumer that does not exist yet, so it never scales the worker from
	// zero and published jobs are never delivered. Returning the error
	// requeues the component until NATS is reachable.
	if r.Streams != nil {
		if err := r.Streams.EnsureStreamAndConsumer(ctx, &comp); err != nil {
			return ctrl.Result{}, fmt.Errorf("provision JetStream: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// apply creates the object, or updates it in place if it already exists.
// On update it first fetches the live object to capture its resourceVersion
// (required by the API server for optimistic concurrency), then carries that
// resourceVersion onto the freshly-rendered object before the Update call.
// SEM@edd453e8c1ef926476270a4ae067018900cad001: create a Kubernetes object or update it in place preserving resourceVersion
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
// SEM@50b942b21c528f6a4405c3ce2dccedfdd379012a: register the reconciler with the controller-runtime manager and declare owned child types
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

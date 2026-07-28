package controller

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
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
//
// Two guards stand between this function and an API-server hot loop, both
// added after production hit one: on the AWS cluster the tmi-chunk-embed and
// tmi-extractor Deployments reached metadata.generation ~2.45 million in six
// days, a measured ~4.3 spec writes per second each, with worker pods
// destroyed and recreated continuously as a side effect.
//
//  1. spec.replicas is preserved from the live object. RenderDeployment
//     deliberately never sets it because KEDA owns it, but "not set" on an
//     Update means nil, and the API server defaults nil to 1 — silently
//     undoing every KEDA scale-to-zero. The renderer cannot preserve what it
//     does not know, so the preservation has to happen here.
//  2. The Update is skipped entirely when the live object already satisfies
//     the rendered one. Without this, an Update fires even when nothing
//     changed, and because SetupWithManager Owns(&appsv1.Deployment{}), that
//     write re-triggers the watch, which reconciles, which writes again.
//
// SEM@a1b2c3d4e5f60718293a4b5c6d7e8f9012345678: create a Kubernetes object, or update it in place only when the live object does not already satisfy it
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

	preserveAutoscaledReplicas(obj, existing)

	if liveSatisfies(obj, existing) {
		return nil
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// preserveAutoscaledReplicas carries the live spec.replicas onto a rendered
// Deployment that does not set one, so an Update cannot clobber the value the
// autoscaler owns. A renderer that DOES set replicas is left alone — that is
// an explicit intent to control the count.
// SEM@a1b2c3d4e5f60718293a4b5c6d7e8f9012345678: copy the live replica count onto a rendered Deployment that leaves it unset (pure)
func preserveAutoscaledReplicas(rendered, live client.Object) {
	d, ok := rendered.(*appsv1.Deployment)
	if !ok || d.Spec.Replicas != nil {
		return
	}
	if lv, ok := live.(*appsv1.Deployment); ok {
		d.Spec.Replicas = lv.Spec.Replicas
	}
}

// liveSatisfies reports whether the live object already matches what was
// rendered, so the Update can be skipped.
//
// Comparison is DeepDerivative rather than DeepEqual: fields unset in the
// rendered object are treated as "don't care". That is what makes the check
// stable against API-server defaulting — live carries dozens of defaulted
// fields (terminationMessagePath, dnsPolicy, revisionHistoryLimit, …) that
// the renderer never mentions, and DeepEqual would report a difference on
// every single pass, which is the loop this exists to prevent.
//
// The trade-off is that clearing a field cannot be expressed by omitting it.
// These renderers build a fixed shape from the TMIComponent spec and never
// need to clear anything, so the trade is sound here. An unrecognised type
// returns false — always update — because a wrong "no change" is a stuck
// controller, while a redundant update is merely wasteful.
// SEM@a1b2c3d4e5f60718293a4b5c6d7e8f9012345678: report whether a live Kubernetes object already satisfies the rendered one (pure)
func liveSatisfies(rendered, live client.Object) bool {
	switch d := rendered.(type) {
	case *appsv1.Deployment:
		lv, ok := live.(*appsv1.Deployment)
		return ok && apiequality.Semantic.DeepDerivative(d.Spec, lv.Spec)
	case *networkingv1.NetworkPolicy:
		lv, ok := live.(*networkingv1.NetworkPolicy)
		return ok && apiequality.Semantic.DeepDerivative(d.Spec, lv.Spec)
	default:
		return false
	}
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

package controller

import (
	"strconv"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// natsMonitoringEndpoint is the in-cluster NATS monitoring host:port KEDA's
// nats-jetstream scaler queries for stream/consumer pending counts. It must
// NOT include a scheme: KEDA's scaler prepends "http://" (or "https://" when
// useHttps is set) itself, so a scheme here produces a doubled
// "http://http://..." URL whose host fails DNS resolution and leaves the
// queue-depth metric perpetually unavailable (HPA shows <unknown>).
const natsMonitoringEndpoint = "nats.tmi-platform.svc:8222"

// RenderScaledObject builds the KEDA ScaledObject for a component as an
// unstructured object, avoiding a KEDA Go-module dependency. KEDA scales
// the worker Deployment on JetStream pending-message depth.
func RenderScaledObject(c *platformv1alpha1.TMIComponent) *unstructured.Unstructured {
	// One subject -> one stream/consumer pair (see render_jetstream.go).
	streamName := streamNameFor(c)
	consumerName := consumerNameFor(c)

	so := &unstructured.Unstructured{}
	so.SetAPIVersion("keda.sh/v1alpha1")
	so.SetKind("ScaledObject")
	so.SetName(c.Name)
	so.SetNamespace(c.Namespace)
	so.Object["spec"] = map[string]interface{}{
		"scaleTargetRef":  map[string]interface{}{"name": c.Name},
		"minReplicaCount": int64(c.Spec.Scaling.MinReplicas),
		"maxReplicaCount": int64(c.Spec.Scaling.MaxReplicas),
		"triggers": []interface{}{
			map[string]interface{}{
				"type": "nats-jetstream",
				"metadata": map[string]interface{}{
					"natsServerMonitoringEndpoint": natsMonitoringEndpoint,
					"account":                      "$G",
					"stream":                       streamName,
					"consumer":                     consumerName,
					"lagThreshold":                 strconv.Itoa(int(c.Spec.Scaling.QueueDepthTarget)),
				},
			},
		},
	}
	return so
}

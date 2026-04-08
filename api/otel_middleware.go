package api

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// OTelSpanEnrichmentMiddleware adds TMI-specific attributes to the active OTel span.
// Must be placed after auth middleware so user context is available.
func OTelSpanEnrichmentMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		span := trace.SpanFromContext(c.Request.Context())
		if !span.IsRecording() {
			return
		}

		var attrs []attribute.KeyValue

		if userID, exists := c.Get("userID"); exists {
			if id, ok := userID.(string); ok && id != "" {
				attrs = append(attrs, attribute.String("tmi.user.id", id))
			}
		}

		if tmID, exists := c.Get("threatModelID"); exists {
			if id, ok := tmID.(string); ok && id != "" {
				attrs = append(attrs, attribute.String("tmi.threat_model.id", id))
			}
		}

		if diagID, exists := c.Get("diagramID"); exists {
			if id, ok := diagID.(string); ok && id != "" {
				attrs = append(attrs, attribute.String("tmi.diagram.id", id))
			}
		}

		if reqID, exists := c.Get("requestID"); exists {
			if id, ok := reqID.(string); ok && id != "" {
				attrs = append(attrs, attribute.String("tmi.request.id", id))
			}
		}

		if len(attrs) > 0 {
			span.SetAttributes(attrs...)
		}
	}
}

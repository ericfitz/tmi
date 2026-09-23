package api

import (
	"github.com/gin-gonic/gin"
)

// Webhook Delivery Methods - ServerInterface Implementation

// GetWebhookDeliveryStatus retrieves a webhook delivery record (dual auth: JWT or HMAC)
// SEM@ca61a567c4babc9270ee913396aaa4fb530505a3: fetch the delivery status record for a webhook delivery by ID (reads DB)
func (s *Server) GetWebhookDeliveryStatus(c *gin.Context, deliveryId DeliveryId, params GetWebhookDeliveryStatusParams) {
	// Delegate to standalone handler
	GetWebhookDeliveryStatus(c)
}

// UpdateWebhookDeliveryStatus updates delivery status (HMAC authenticated)
// SEM@ca61a567c4babc9270ee913396aaa4fb530505a3: update the delivery status of a webhook delivery record (reads DB)
func (s *Server) UpdateWebhookDeliveryStatus(c *gin.Context, deliveryId DeliveryId, params UpdateWebhookDeliveryStatusParams) {
	// Delegate to standalone handler
	UpdateWebhookDeliveryStatus(c)
}

// CancelWebhookDelivery cancels a delivery (JWT authenticated) (#913)
// SEM@411a53c663401d55a0f66913e00979599a208c93: delegate delivery cancellation to the standalone handler
func (s *Server) CancelWebhookDelivery(c *gin.Context, deliveryId DeliveryId) {
	CancelWebhookDelivery(c)
}

// ListMyWebhookDeliveries lists the caller's visible deliveries (#913)
// SEM@411a53c663401d55a0f66913e00979599a208c93: delegate the caller-scoped delivery list to the standalone handler
func (s *Server) ListMyWebhookDeliveries(c *gin.Context, params ListMyWebhookDeliveriesParams) {
	ListMyWebhookDeliveries(c, params)
}

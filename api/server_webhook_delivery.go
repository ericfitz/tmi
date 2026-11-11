package api

import (
	"github.com/gin-gonic/gin"
)

// Webhook Delivery Methods - ServerInterface Implementation

// GetWebhookDeliveryStatus retrieves a webhook delivery record (dual auth: JWT or HMAC)
func (s *Server) GetWebhookDeliveryStatus(c *gin.Context, deliveryId DeliveryId, params GetWebhookDeliveryStatusParams) {
	// Delegate to standalone handler
	GetWebhookDeliveryStatus(c)
}

// UpdateWebhookDeliveryStatus updates delivery status (HMAC authenticated)
func (s *Server) UpdateWebhookDeliveryStatus(c *gin.Context, deliveryId DeliveryId, params UpdateWebhookDeliveryStatusParams) {
	// Delegate to standalone handler
	UpdateWebhookDeliveryStatus(c)
}

// CancelWebhookDelivery cancels a delivery (JWT authenticated) (#913)
func (s *Server) CancelWebhookDelivery(c *gin.Context, deliveryId DeliveryId) {
	CancelWebhookDelivery(c)
}

// ListMyWebhookDeliveries lists the caller's visible deliveries (#913)
func (s *Server) ListMyWebhookDeliveries(c *gin.Context, params ListMyWebhookDeliveriesParams) {
	ListMyWebhookDeliveries(c, params)
}

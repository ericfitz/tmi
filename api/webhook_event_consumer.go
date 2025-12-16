package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// WebhookEventConsumer consumes events from Redis Streams and creates webhook deliveries
type WebhookEventConsumer struct {
	redisClient *redis.Client
	streamKey   string
	groupName   string
	consumerID  string
	running     bool
	stopChan    chan struct{}
}

// NewWebhookEventConsumer creates a new event consumer
func NewWebhookEventConsumer(redisClient *redis.Client, streamKey, groupName, consumerID string) *WebhookEventConsumer {
	return &WebhookEventConsumer{
		redisClient: redisClient,
		streamKey:   streamKey,
		groupName:   groupName,
		consumerID:  consumerID,
		stopChan:    make(chan struct{}),
	}
}

// Start begins consuming events from the Redis Stream
func (c *WebhookEventConsumer) Start(ctx context.Context) error {
	logger := slogging.Get()

	if c.redisClient == nil {
		logger.Warn("Redis client not available, webhook event consumer disabled")
		return nil
	}

	// Create consumer group if it doesn't exist
	err := c.redisClient.XGroupCreateMkStream(ctx, c.streamKey, c.groupName, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		logger.Error("failed to create consumer group: %v", err)
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	c.running = true
	logger.Info("webhook event consumer started (stream: %s, group: %s, consumer: %s)", c.streamKey, c.groupName, c.consumerID)

	// Start consuming in a goroutine
	go c.consumeLoop(ctx)

	return nil
}

// Stop gracefully stops the consumer
func (c *WebhookEventConsumer) Stop() {
	logger := slogging.Get()
	if c.running {
		c.running = false
		close(c.stopChan)
		logger.Info("webhook event consumer stopped")
	}
}

// consumeLoop continuously reads and processes events
func (c *WebhookEventConsumer) consumeLoop(ctx context.Context) {
	logger := slogging.Get()

	for c.running {
		select {
		case <-ctx.Done():
			logger.Info("context cancelled, stopping event consumer")
			return
		case <-c.stopChan:
			logger.Info("stop signal received, stopping event consumer")
			return
		default:
			// Read messages from the stream
			streams, err := c.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    c.groupName,
				Consumer: c.consumerID,
				Streams:  []string{c.streamKey, ">"},
				Count:    50,
				Block:    5 * time.Second,
			}).Result()

			if err != nil {
				if err == redis.Nil {
					// No new messages, continue
					continue
				}
				logger.Error("failed to read from stream: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			// Process each message
			for _, stream := range streams {
				for _, message := range stream.Messages {
					if err := c.processMessage(ctx, message); err != nil {
						logger.Error("failed to process message %s: %v", message.ID, err)
						// Continue processing other messages
					} else {
						// Acknowledge the message
						if err := c.redisClient.XAck(ctx, c.streamKey, c.groupName, message.ID).Err(); err != nil {
							logger.Error("failed to acknowledge message %s: %v", message.ID, err)
						}
					}
				}
			}
		}
	}
}

// processMessage processes a single event message
func (c *WebhookEventConsumer) processMessage(ctx context.Context, message redis.XMessage) error {
	logger := slogging.Get()

	// Extract event data from message
	eventType, ok := message.Values["event_type"].(string)
	if !ok {
		return fmt.Errorf("invalid event_type in message")
	}

	resourceID, ok := message.Values["resource_id"].(string)
	if !ok {
		return fmt.Errorf("invalid resource_id in message")
	}

	ownerID, ok := message.Values["owner_id"].(string)
	if !ok {
		return fmt.Errorf("invalid owner_id in message")
	}

	payloadStr, ok := message.Values["payload"].(string)
	if !ok {
		return fmt.Errorf("invalid payload in message")
	}

	logger.Debug("processing webhook event: %s for resource %s (owner: %s)", eventType, resourceID, ownerID)

	// Find all active subscriptions for this owner
	ownerUUID, err := uuid.Parse(ownerID)
	if err != nil {
		return fmt.Errorf("invalid owner UUID: %w", err)
	}

	subscriptions, err := GlobalWebhookSubscriptionStore.ListActiveByOwner(ownerUUID.String())
	if err != nil {
		return fmt.Errorf("failed to list subscriptions: %w", err)
	}

	// Filter subscriptions that match the event type and threat model
	var payload EventPayload
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	matchingSubscriptions := c.filterSubscriptions(subscriptions, eventType, payload.ThreatModelID)

	logger.Debug("found %d matching subscriptions for event %s", len(matchingSubscriptions), eventType)

	// Create delivery records for each matching subscription
	for _, sub := range matchingSubscriptions {
		if err := c.createDelivery(ctx, sub, eventType, payloadStr); err != nil {
			logger.Error("failed to create delivery for subscription %s: %v", sub.Id, err)
			// Continue with other subscriptions
		}
	}

	return nil
}

// filterSubscriptions filters subscriptions based on event type and threat model
func (c *WebhookEventConsumer) filterSubscriptions(subscriptions []DBWebhookSubscription, eventType, threatModelID string) []DBWebhookSubscription {
	var filtered []DBWebhookSubscription

	for _, sub := range subscriptions {
		// Check if subscription matches event type
		eventMatches := false
		for _, e := range sub.Events {
			if e == eventType || e == "*" {
				eventMatches = true
				break
			}
		}

		if !eventMatches {
			continue
		}

		// Check threat model filter
		// If subscription has no threat model filter (nil), it matches all threat models
		// If subscription has a threat model filter, it must match
		if sub.ThreatModelId != nil {
			if threatModelID == "" || sub.ThreatModelId.String() != threatModelID {
				continue
			}
		}

		filtered = append(filtered, sub)
	}

	return filtered
}

// createDelivery creates a webhook delivery record
func (c *WebhookEventConsumer) createDelivery(ctx context.Context, subscription DBWebhookSubscription, eventType, payload string) error {
	logger := slogging.Get()

	// Generate UUIDv7 for delivery ID (time-ordered)
	deliveryID := uuid.Must(uuid.NewV7())

	delivery := DBWebhookDelivery{
		Id:             deliveryID,
		SubscriptionId: subscription.Id,
		EventType:      eventType,
		Payload:        payload,
		Status:         "pending",
		Attempts:       0,
		CreatedAt:      time.Now().UTC(),
	}

	_, err := GlobalWebhookDeliveryStore.Create(delivery)
	if err != nil {
		return fmt.Errorf("failed to create delivery record: %w", err)
	}

	logger.Debug("created delivery %s for subscription %s", deliveryID, subscription.Id)
	return nil
}

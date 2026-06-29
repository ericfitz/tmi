package notifications

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/lib/pq"
)

// PostgresNotifier implements NotificationService using PostgreSQL's LISTEN/NOTIFY
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: NotificationService backed by PostgreSQL LISTEN/NOTIFY with reconnect support
type PostgresNotifier struct {
	connStr    string
	db         *sql.DB
	listener   *pq.Listener
	mu         sync.RWMutex
	channels   map[string][]chan Notification
	stopChan   chan struct{}
	running    bool
	logger     *slogging.Logger
	reconnects int
}

// NewPostgresNotifier creates a new PostgreSQL notification service
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: build and start a PostgreSQL LISTEN/NOTIFY notifier (reads DB)
func NewPostgresNotifier(connStr string, db *sql.DB) (*PostgresNotifier, error) {
	logger := slogging.Get()
	logger.Debug("Initializing PostgreSQL notification service")

	notifier := &PostgresNotifier{
		connStr:  connStr,
		db:       db,
		channels: make(map[string][]chan Notification),
		stopChan: make(chan struct{}),
		logger:   logger,
	}

	// Create the pq.Listener
	listener := pq.NewListener(connStr, 10*time.Second, time.Minute, notifier.eventCallback)
	notifier.listener = listener

	// Start the listener goroutine
	go notifier.listenLoop()

	notifier.running = true
	logger.Info("PostgreSQL notification service initialized")

	return notifier, nil
}

// eventCallback handles listener events
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: handle pq listener connection lifecycle events and trigger channel resubscription
func (p *PostgresNotifier) eventCallback(ev pq.ListenerEventType, err error) {
	switch ev {
	case pq.ListenerEventConnected:
		p.logger.Info("PostgreSQL listener connected")
		p.reconnects = 0
	case pq.ListenerEventDisconnected:
		p.logger.Warn("PostgreSQL listener disconnected: %v", err)
	case pq.ListenerEventReconnected:
		p.reconnects++
		p.logger.Info("PostgreSQL listener reconnected (attempt %d)", p.reconnects)
		// Re-subscribe to all channels
		p.resubscribeAll()
	case pq.ListenerEventConnectionAttemptFailed:
		p.logger.Error("PostgreSQL listener connection attempt failed: %v", err)
	}
}

// resubscribeAll re-subscribes to all channels after reconnection
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: re-register all active LISTEN subscriptions after a listener reconnect (reads DB)
func (p *PostgresNotifier) resubscribeAll() {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for channel := range p.channels {
		if err := p.listener.Listen(channel); err != nil {
			p.logger.Error("Failed to re-subscribe to channel %s: %v", channel, err)
		} else {
			p.logger.Debug("Re-subscribed to channel %s", channel)
		}
	}
}

// listenLoop processes incoming notifications
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: dispatch incoming PostgreSQL notifications and ping to maintain the connection
func (p *PostgresNotifier) listenLoop() {
	p.logger.Debug("Starting PostgreSQL notification listener loop")

	for {
		select {
		case <-p.stopChan:
			p.logger.Info("PostgreSQL notification listener stopped")
			return
		case n := <-p.listener.Notify:
			if n == nil {
				// Connection issue, wait for reconnect
				continue
			}
			p.handleNotification(n)
		case <-time.After(90 * time.Second):
			// Ping to maintain connection
			go func() {
				if err := p.listener.Ping(); err != nil {
					p.logger.Error("PostgreSQL listener ping failed: %v", err)
				}
			}()
		}
	}
}

// handleNotification distributes a notification to subscribers
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: fan out a received PostgreSQL notification to all channel subscribers (mutates shared state)
func (p *PostgresNotifier) handleNotification(n *pq.Notification) {
	p.mu.RLock()
	subscribers, exists := p.channels[n.Channel]
	p.mu.RUnlock()

	if !exists || len(subscribers) == 0 {
		p.logger.Debug("No subscribers for channel %s", n.Channel)
		return
	}

	notification := Notification{
		Channel:   n.Channel,
		Payload:   n.Extra,
		Timestamp: time.Now().UTC(),
	}

	dispatchToSubscribers(subscribers, notification, n.Channel, p.logger)
}

// Subscribe implements NotificationService.Subscribe
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: register a subscriber channel for a named notification channel, starting LISTEN if first (reads DB)
func (p *PostgresNotifier) Subscribe(ctx context.Context, channel string) (<-chan Notification, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create a new notification channel for this subscriber
	notifyChan := make(chan Notification, 100)

	// Check if we need to start listening to this channel
	needsListen := len(p.channels[channel]) == 0

	// Add subscriber
	p.channels[channel] = append(p.channels[channel], notifyChan)

	if needsListen {
		if err := p.listener.Listen(channel); err != nil {
			// Remove the subscriber we just added
			p.channels[channel] = p.channels[channel][:len(p.channels[channel])-1]
			close(notifyChan)
			return nil, fmt.Errorf("failed to listen on channel %s: %w", channel, err)
		}
		p.logger.Info("Started listening on PostgreSQL channel: %s", channel)
	}

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		p.unsubscribe(channel, notifyChan)
	}()

	return notifyChan, nil
}

// unsubscribe removes a subscriber from a channel
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: remove a subscriber and UNLISTEN the PostgreSQL channel when no subscribers remain (mutates shared state)
func (p *PostgresNotifier) unsubscribe(channel string, notifyChan chan Notification) {
	p.mu.Lock()
	defer p.mu.Unlock()

	subscribers := p.channels[channel]
	for i, ch := range subscribers {
		if ch == notifyChan {
			// Remove this subscriber
			p.channels[channel] = append(subscribers[:i], subscribers[i+1:]...)
			close(notifyChan)
			break
		}
	}

	// Unlisten if no more subscribers
	if len(p.channels[channel]) == 0 {
		if err := p.listener.Unlisten(channel); err != nil {
			p.logger.Error("Failed to unlisten from channel %s: %v", channel, err)
		} else {
			p.logger.Info("Stopped listening on PostgreSQL channel: %s", channel)
		}
		delete(p.channels, channel)
	}
}

// Notify implements NotificationService.Notify
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: dispatch a PostgreSQL LISTEN/NOTIFY message to a named channel (reads DB)
func (p *PostgresNotifier) Notify(ctx context.Context, channel string, payload string) error {
	query := `SELECT pg_notify($1, $2)`
	_, err := p.db.ExecContext(ctx, query, channel, payload)
	if err != nil {
		p.logger.Error("Failed to send PostgreSQL notification on channel %s: %v", channel, err)
		return fmt.Errorf("failed to send notification: %w", err)
	}
	p.logger.Debug("Sent PostgreSQL notification on channel %s", channel)
	return nil
}

// Close implements NotificationService.Close
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: stop the notification service, close all subscriber channels, and release the DB listener (mutates shared state)
func (p *PostgresNotifier) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return nil
	}

	p.running = false
	close(p.stopChan)

	// Close all subscriber channels
	for channel, subscribers := range p.channels {
		for _, ch := range subscribers {
			close(ch)
		}
		delete(p.channels, channel)
	}

	// Close the listener
	if p.listener != nil {
		if err := p.listener.Close(); err != nil {
			p.logger.Error("Error closing PostgreSQL listener: %v", err)
			return err
		}
	}

	p.logger.Info("PostgreSQL notification service closed")
	return nil
}

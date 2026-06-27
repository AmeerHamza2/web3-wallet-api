// Package events defines the domain-event contract for the wallet service. In
// production Publisher would be backed by a message broker (Kafka, NATS,
// RabbitMQ); LogPublisher is the local/test implementation.
package events

import (
	"context"
	"log/slog"
	"time"
)

// Event types emitted by the service.
const (
	TypeWalletCreated = "wallet.created"
	TypeTxSubmitted   = "transaction.submitted"
	TypeTxFailed      = "transaction.failed"
)

// Event is a single domain event destined for the message bus.
type Event struct {
	Type    string         `json:"type"`
	Time    time.Time      `json:"time"`
	Payload map[string]any `json:"payload"`
}

// Publisher publishes domain events. Implementations must be safe for
// concurrent use; Publish is best-effort fire-and-forget for callers.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// LogPublisher writes events to the structured logger. It is the default
// transport for local development and tests, standing in for a real broker.
type LogPublisher struct {
	log *slog.Logger
}

// NewLogPublisher returns a Publisher that logs each event as a structured line.
func NewLogPublisher(log *slog.Logger) *LogPublisher {
	return &LogPublisher{log: log}
}

// Publish records the event. It never returns an error for the log backend.
func (p *LogPublisher) Publish(_ context.Context, e Event) error {
	p.log.Info("domain event published",
		slog.String("event_type", e.Type),
		slog.Time("event_time", e.Time),
		slog.Any("payload", e.Payload),
	)
	return nil
}

// New is a convenience constructor for an Event stamped at t.
func New(eventType string, t time.Time, payload map[string]any) Event {
	return Event{Type: eventType, Time: t, Payload: payload}
}

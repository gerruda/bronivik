package events

import (
	"encoding/json"
	"sync"
	"time"
)

const (
	EventBookingCreated    = "booking_created"
	EventBookingConfirmed  = "booking_confirmed"
	EventBookingCanceled   = "booking_canceled"
	EventBookingCompleted  = "booking_completed"
	EventBookingItemChange = "booking_item_changed"
)

// BookingEventPayload describes the minimal booking snapshot for event consumers.
type BookingEventPayload struct {
	BookingID   int64     `json:"booking_id"`
	UserID      int64     `json:"user_id"`
	UserName    string    `json:"user_name"`
	ItemID      int64     `json:"item_id"`
	ItemName    string    `json:"item_name"`
	Status      string    `json:"status"`
	Date        time.Time `json:"date"`
	Comment     string    `json:"comment,omitempty"`
	ChangedBy   string    `json:"changed_by,omitempty"`
	ChangedByID int64     `json:"changed_by_id,omitempty"`
}

// Event represents a lightweight domain event.
type Event struct {
	ID        int64
	Type      string
	Payload   []byte
	CreatedAt time.Time
	Processed bool
}

// EventHandler reacts to an event.
type EventHandler func(event *Event) error

// EventBus provides in-process pub/sub for events.
type EventBus struct {
	subscribers map[string][]EventHandler
	mu          sync.RWMutex
}

// NewEventBus constructs an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[string][]EventHandler)}
}

// Subscribe registers a handler for a given event type.
func (b *EventBus) Subscribe(eventType string, handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[eventType] = append(b.subscribers[eventType], handler)
}

// Publish notifies subscribers of the event type.
func (b *EventBus) Publish(event *Event) {
	b.mu.RLock()
	handlers := append([]EventHandler(nil), b.subscribers[event.Type]...)
	b.mu.RUnlock()

	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	for _, handler := range handlers {
		// Handlers run synchronously; caller decides concurrency model.
		_ = handler(event)
	}
}

// PublishJSON serializes the payload and publishes an event.
func (b *EventBus) PublishJSON(eventType string, payload interface{}) error {
	if b == nil {
		return nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	b.Publish(&Event{Type: eventType, Payload: raw, CreatedAt: time.Now()})
	return nil
}

// NewJSONEvent builds an Event with JSON payload for manual publishing.
func NewJSONEvent(eventType string, payload interface{}) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}

	return Event{Type: eventType, Payload: raw, CreatedAt: time.Now()}, nil
}

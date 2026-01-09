package events

import (
	"encoding/json"
	"testing"
)

func TestEventBus(t *testing.T) {
	bus := NewEventBus()

	var received *Event
	var callCount int

	handler := func(event *Event) error {
		received = event
		callCount++
		return nil
	}

	bus.Subscribe("test_event", handler)

	payload := map[string]string{"foo": "bar"}
	err := bus.PublishJSON("test_event", payload)
	if err != nil {
		t.Fatalf("PublishJSON failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	if received.Type != "test_event" {
		t.Errorf("expected type test_event, got %s", received.Type)
	}

	var decoded map[string]string
	if err := json.Unmarshal(received.Payload, &decoded); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}

	if decoded["foo"] != "bar" {
		t.Errorf("expected foo=bar, got %s", decoded["foo"])
	}
}

func TestEventBusMultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	var count1, count2 int

	bus.Subscribe("event", func(_ *Event) error { count1++; return nil })
	bus.Subscribe("event", func(_ *Event) error { count2++; return nil })

	bus.Publish(&Event{Type: "event"})

	if count1 != 1 || count2 != 1 {
		t.Errorf("expected both handlers to be called once, got %d and %d", count1, count2)
	}
}

func TestEventBusNoSubscribers(t *testing.T) {
	bus := NewEventBus()
	// Should not panic
	bus.Publish(&Event{Type: "unknown"})
	err := bus.PublishJSON("unknown", nil)
	if err != nil {
		t.Errorf("PublishJSON failed: %v", err)
	}
}

func TestNewJSONEvent(t *testing.T) {
	payload := BookingEventPayload{BookingID: 123}
	event, err := NewJSONEvent("type", payload)
	if err != nil {
		t.Fatalf("NewJSONEvent failed: %v", err)
	}

	if event.Type != "type" {
		t.Errorf("expected type, got %s", event.Type)
	}

	if event.CreatedAt.IsZero() {
		t.Errorf("expected CreatedAt to be set")
	}

	var decoded BookingEventPayload
	if err := json.Unmarshal(event.Payload, &decoded); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if decoded.BookingID != 123 {
		t.Errorf("expected BookingID 123, got %d", decoded.BookingID)
	}
}

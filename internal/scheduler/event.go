package scheduler

import (
	"context"
	"fmt"
	"sync"
)

const EventBrokerSubscriberDefaultBufferSize = 10

// Event is the structure of the event that would be emitted by the processes
type Event struct {
	// Name is the name of the event which would be subscribed by the listeners here processes
	Name string
	// Payload is the payload of the event which would be used by the listeners to read the data
	Payload interface{}
	// Source is the name of the process that would emit this event
	Source string
}

// EventBroker is an in-memory event broker which would be used to emit and listen to the events
type EventBroker struct {
	// subscribers is a map of event name and a event channel which would be used to listen to the events
	subscribers map[string][]chan Event
	// mu is the mutex for the event broker
	mu sync.RWMutex
}

// NewEventBroker would return a new instance of the event broker
func NewEventBroker() *EventBroker {
	return &EventBroker{
		subscribers: make(map[string][]chan Event),
		mu:          sync.RWMutex{},
	}
}

// Subscribe would take in the eventName and would add a new channel to the subscribers map and return the channel
// this new channel would be used by the process to listen to the event
func (b *EventBroker) Subscribe(eventName string) <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, EventBrokerSubscriberDefaultBufferSize)
	b.subscribers[eventName] = append(b.subscribers[eventName], ch)
	return ch
}

// Publish would take in the event and would emit the event to all the listeners. Would iterate over the subscribers array of the
// event name and emit the event to all the listeners
func (b *EventBroker) Publish(event Event, ctx context.Context) error {
    b.mu.RLock()
    defer b.mu.RUnlock()

    if subscribers, found := b.subscribers[event.Name]; found {
        for _, ch := range subscribers {
            select {
            case ch <- event:
                // Successfully sent the event.
            case <-ctx.Done():
                return ctx.Err()
            default:
                fmt.Printf("Warning: Subscriber not consuming event %s\n", event.Name)
            }
        }
    }
    return nil
}

// Close cleans up all channels to prevent leaks.
func (b *EventBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for eventName, channels := range b.subscribers {
		for _, ch := range channels {
			select {
			case <-ch:
				// Channel is already closed, skip closing it again.
			default:
				close(ch)
			}
		}
		delete(b.subscribers, eventName)
	}
}

func (b *EventBroker) Unsubscribe(eventName string, ch <-chan Event) {
    b.mu.Lock()
    defer b.mu.Unlock()

    if subscribers, found := b.subscribers[eventName]; found {
        for i, subscriber := range subscribers {
            if subscriber == ch {
                b.subscribers[eventName] = append(subscribers[:i], subscribers[i+1:]...)
				close(subscriber)
                break
            }
        }
    }
}

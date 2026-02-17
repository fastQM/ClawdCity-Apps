package network

import (
	"sync"
)

// MemoryPubSub is a process-local transport used for MVP development/testing.
type MemoryPubSub struct {
	mu     sync.RWMutex
	nextID int
	subs   map[string]map[int]chan Message
}

func NewMemoryPubSub() *MemoryPubSub {
	return &MemoryPubSub{subs: make(map[string]map[int]chan Message)}
}

func (m *MemoryPubSub) Publish(topic string, payload []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.subs[topic] {
		msg := Message{Topic: topic, Payload: append([]byte(nil), payload...)}
		select {
		case ch <- msg:
		default:
			// Non-blocking send to avoid one slow subscriber stalling all publishers.
		}
	}
	return nil
}

func (m *MemoryPubSub) Subscribe(topic string) (<-chan Message, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.subs[topic]; !ok {
		m.subs[topic] = make(map[int]chan Message)
	}
	id := m.nextID
	m.nextID++
	ch := make(chan Message, 64)
	m.subs[topic][id] = ch

	cancel := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if subsByTopic, ok := m.subs[topic]; ok {
			if sub, exists := subsByTopic[id]; exists {
				delete(subsByTopic, id)
				close(sub)
			}
			if len(subsByTopic) == 0 {
				delete(m.subs, topic)
			}
		}
	}
	return ch, cancel, nil
}

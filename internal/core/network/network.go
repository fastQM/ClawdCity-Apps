package network

// Message is the transport envelope used by the runtime.
type Message struct {
	Topic   string
	Payload []byte
}

// PubSub is a minimal interface for broadcast-style communication.
type PubSub interface {
	Publish(topic string, payload []byte) error
	Subscribe(topic string) (<-chan Message, func(), error)
}

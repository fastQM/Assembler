package network

import "context"

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

// DirectMessenger is an optional point-to-point stream transport.
type DirectMessenger interface {
	SendDirect(ctx context.Context, peerID string, protocol string, payload []byte) error
	RegisterDirectHandler(protocol string, fn func(peerID string, payload []byte))
}

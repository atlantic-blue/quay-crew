// Package channel defines the contract every Quay System channel implements.
//
// A channel receives input from somewhere (a CLI, a chat app, a scheduler), turns it into an
// InboundMessage, and delivers OutboundMessage replies. The control plane and the rest of the
// system only ever see this contract, so channels are independent and interchangeable.
package channel

import (
	"context"
	"errors"
	"sync"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// Errors returned by adapters.
var (
	ErrNilHandler = errors.New("channel: nil inbound handler")
	ErrNilMessage = errors.New("channel: nil outbound message")
)

// InboundHandler is called by an adapter for each message it receives.
type InboundHandler func(ctx context.Context, msg *quaycrewv1.InboundMessage) error

// Adapter is the contract every channel implements.
type Adapter interface {
	// ID is the channel's stable identifier, for example "cli" or "telegram".
	ID() string
	// Start begins receiving input, calling handler for each inbound message, and returns when
	// ctx is done. It is expected to block for the lifetime of the channel.
	Start(ctx context.Context, handler InboundHandler) error
	// Deliver sends an outbound reply back through the channel.
	Deliver(ctx context.Context, msg *quaycrewv1.OutboundMessage) error
	// Stop releases the channel's resources.
	Stop(ctx context.Context) error
}

// MemoryAdapter is an in memory Adapter for tests and local wiring. It records delivered messages
// and lets a caller inject inbound messages to the registered handler.
type MemoryAdapter struct {
	id string

	ready     chan struct{}
	readyOnce sync.Once

	mu        sync.Mutex
	handler   InboundHandler
	delivered []*quaycrewv1.OutboundMessage
}

// compile time check that MemoryAdapter satisfies Adapter.
var _ Adapter = (*MemoryAdapter)(nil)

// NewMemoryAdapter returns a MemoryAdapter with the given channel id.
func NewMemoryAdapter(id string) *MemoryAdapter {
	return &MemoryAdapter{id: id, ready: make(chan struct{})}
}

// ID returns the channel id.
func (m *MemoryAdapter) ID() string { return m.id }

// Start registers the handler, marks the adapter ready, and blocks until ctx is done.
func (m *MemoryAdapter) Start(ctx context.Context, handler InboundHandler) error {
	if handler == nil {
		return ErrNilHandler
	}
	m.mu.Lock()
	m.handler = handler
	m.mu.Unlock()
	m.readyOnce.Do(func() { close(m.ready) })

	<-ctx.Done()
	return ctx.Err()
}

// Deliver records an outbound message.
func (m *MemoryAdapter) Deliver(_ context.Context, msg *quaycrewv1.OutboundMessage) error {
	if msg == nil {
		return ErrNilMessage
	}
	m.mu.Lock()
	m.delivered = append(m.delivered, msg)
	m.mu.Unlock()
	return nil
}

// Stop is a no op for the in memory adapter.
func (m *MemoryAdapter) Stop(context.Context) error { return nil }

// Inject sends an inbound message to the registered handler, as if it had arrived on the channel.
// It waits until Start has registered a handler, or until ctx is done.
func (m *MemoryAdapter) Inject(ctx context.Context, msg *quaycrewv1.InboundMessage) error {
	if msg == nil {
		return ErrNilMessage
	}
	select {
	case <-m.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	m.mu.Lock()
	handler := m.handler
	m.mu.Unlock()
	return handler(ctx, msg)
}

// Delivered returns a copy of the outbound messages recorded so far.
func (m *MemoryAdapter) Delivered() []*quaycrewv1.OutboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*quaycrewv1.OutboundMessage, len(m.delivered))
	copy(out, m.delivered)
	return out
}

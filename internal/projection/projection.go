// Package projection materialises the read model from the event log.
//
// The log is the write side and this is a consumer of it, which is the whole reason the log carries
// enough to rebuild a conversation without reading anything else. Drop the projected table, start
// again from the beginning of the log, and you get the same answer: the read model is disposable by
// design, and that is what makes it safe to change its shape later.
//
// Delivery is at least once. A record is handed over again whenever a batch is not committed, so
// everything here has to be safe to do twice. Writing a turn is, because the turn carries an id and
// the store collides on it.
package projection

import (
	"context"
	"fmt"
	"log/slog"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/protobuf/proto"
)

// TurnsPattern matches every workspace's turn stream, including workspaces that do not exist yet.
//
// A stream is named "<workspace>.turns", so this is the only subscription that keeps working when a
// workspace is created while the crew is running. A list of topics read at startup would silently
// ignore everything that happened in a workspace made afterwards, and nothing would say so.
const TurnsPattern = `^.+\.turns$`

// Group is the consumer group the projection joins. It is fixed, so restarting the crew resumes from
// the offsets it had rather than replaying the whole log every time.
const Group = "quaycrew-projection"

// Projection consumes turn events and writes them into the store.
type Projection struct {
	log    messaging.EventLog
	store  store.Store
	logger *slog.Logger
}

// New builds a projection over an event log and the store it materialises into.
func New(log messaging.EventLog, durable store.Store, logger *slog.Logger) *Projection {
	if logger == nil {
		logger = slog.Default()
	}
	return &Projection{log: log, store: durable, logger: logger}
}

// Run consumes until ctx is done, and returns why it stopped.
//
// It blocks, so a caller that wants it in the background runs it in a goroutine. It is deliberately
// not started for you by New: something has to own the lifetime, and hiding a goroutine inside a
// constructor makes that impossible to see.
func (p *Projection) Run(ctx context.Context) error {
	p.logger.Info("projection consuming", "pattern", TurnsPattern, "group", Group)
	return p.log.ConsumePattern(ctx, Group, TurnsPattern, p.handle)
}

// handle writes one record into the read model.
//
// A record that cannot be decoded is dropped with a warning rather than returned as an error. An
// error here stops consumption and the batch is delivered again, so a single unreadable record would
// wedge the projection forever and nothing after it would ever be read. A write that fails is a
// different matter: that is worth retrying, so it is returned.
func (p *Projection) handle(ctx context.Context, record messaging.Record) error {
	event := &quaycrewv1.TurnEvent{}
	if err := proto.Unmarshal(record.Value, event); err != nil {
		p.logger.Warn("a record on the turn stream does not decode, so it is skipped",
			"topic", record.Topic, "error", err)
		return nil
	}
	if event.GetId() == "" || event.GetSession() == "" {
		p.logger.Warn("a turn event names no id or no session, so it is skipped", "topic", record.Topic)
		return nil
	}

	turn := &quaycrewv1.Turn{
		Id:         event.GetId(),
		Session:    event.GetSession(),
		Prompt:     event.GetPrompt(),
		Reply:      event.GetReply(),
		Status:     event.GetStatus(),
		Failure:    event.GetFailure(),
		OccurredAt: event.GetOccurredAt(),
	}
	if err := p.store.AppendTurn(ctx, turn, event.GetWorkspace(), event.GetProject(), event.GetThreadId()); err != nil {
		return fmt.Errorf("projection: append turn %s: %w", event.GetId(), err)
	}
	return nil
}

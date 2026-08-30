package controlplane

import (
	"context"
	"errors"
	"log/slog"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// What a dispatch waits on between writing a session row and the first task running. Each wait is
// named, because "the dispatch failed" sends the reader to the whole path and one of these sends
// them to a component.
const (
	// waitEventLog is the export of one record to the broker. It is on this list because it is where
	// a dispatch stopped: the record sat in the producer and the call never came back.
	waitEventLog = "the event log to take a record"
	// waitStartAhead is the sandbox start already running, because the system starts one at a time.
	waitStartAhead = "the sandbox start ahead of this one"
	// waitContainer is the provider making the container.
	waitContainer = "the sandbox to be created"
	// waitSetup is the secrets, the skills and the signing set up inside it.
	waitSetup = "the sandbox to be set up"
	// waitStoreWrite is the store taking a write, which is what a health check proves.
	waitStoreWrite = "the store to take a write"
)

// startWait is the whole budget from a session row to a sandbox ready for its first task: the start
// ahead of this one, the container, and the setup inside it. One budget rather than one each, so the
// longest a dispatch can take to fail is this and never the sum of them.
//
// Measured rather than chosen. `docker run --rm quaycrew-sandbox-claude:local echo ok` answered in
// about two seconds on the machine that wedged, and in continuous integration the whole first
// dispatch, container start and task included, lands inside six and a half (run 33092308502,
// 16:19:02.893 to 16:19:09.277). This is ten times the slower of the two, which gives a loaded
// machine and a daemon pulling an image room, and still fails long before an operator gives up.
const startWait = 60 * time.Second

// tidyWait is what tearing a half made sandbox down is given, after its start has already run out.
// The dispatch has failed by then and somebody is waiting to be told, so it is short: removing a
// container is one call to the daemon, and the daemon on the machine this fault was found on ran a
// container end to end in about two seconds.
const tidyWait = 10 * time.Second

// exportWait is what one record's export to the event log is given. The log is a copy: the store
// already holds the record, so an export that cannot land is dropped and logged, which is what this
// path always said it did. A broker on the same machine answers in milliseconds.
const exportWait = 5 * time.Second

// waited runs one step of the dispatch path, says in the log that it is waiting, and fails by name
// when the budget runs out.
//
// The line before the step is the point of it. The fault this exists for was silent: the call
// stopped between two lines that were never written, so the log said nothing at all and the system
// looked healthy. A wait now leaves "the system is waiting" with nothing saying it ended.
func waited(ctx context.Context, session, what string, step func(context.Context) error) error {
	slog.InfoContext(ctx, "the system is waiting", "session", session, "for", what)
	began := time.Now()
	err := step(ctx)
	took := time.Since(began)
	if err == nil {
		slog.InfoContext(ctx, "the system waited", "session", session, "for", what, "took", took.String())
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		slog.ErrorContext(ctx, "the system gave up waiting", "session", session, "for", what,
			"took", took.String(), "error", err)
		return status.Errorf(codes.DeadlineExceeded, "waited %s for %s and gave up", took.Round(time.Second), what)
	}
	return err
}

// startSandbox is the path from a session row to a sandbox ready for its first task, under one
// budget so it always ends.
//
// Without the budget the path had no end at all. A control plane that had survived the machine
// running out of memory served every read normally and answered no dispatch: the row was written,
// no task was recorded, no container was made, and the caller stayed inside the call until it was
// killed. See issue 400.
func (s *Server) startSandbox(ctx context.Context, session *quaycrewv1.Session) (sandbox.Sandbox, error) {
	ctx, giveUp := context.WithTimeout(ctx, s.startWait)
	defer giveUp()
	return s.sandboxFor(ctx, session)
}

// export offers one record to the event log under a budget, keyed by session so one session's
// records stay in order on one partition.
//
// The budget is the whole of issue 400. A producer whose broker accepts a connection and answers
// nothing keeps a record without limit by default, and this call carries a context that was
// deliberately detached from the caller's, so there was nothing left to stop it. The dispatch that
// wrote the session row never came back, and the export it was held by is a copy of a record the
// store already holds.
func (s *Server) export(ctx context.Context, sessionID, topic string, value []byte) error {
	ctx, giveUp := context.WithTimeout(ctx, s.exportWait)
	defer giveUp()
	return waited(ctx, sessionID, waitEventLog, func(ctx context.Context) error {
		return s.events.Publish(ctx, topic, []byte(sessionID), value)
	})
}

// takeStart takes the system's one start slot, or gives up when the budget runs out. The slot is
// released by whoever took it.
func (s *Server) takeStart(ctx context.Context) error {
	select {
	case s.starts <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// holding says whether this process already holds a sandbox for the session, which decides whether a
// failed setup may close the container: closing one this process was already accountable for would
// take a live conversation down over a transient failure.
func (s *Server) holding(sessionID string) bool {
	s.sandboxesMu.Lock()
	defer s.sandboxesMu.Unlock()
	_, held := s.sandboxes[sessionID]
	return held
}

// keepSandbox remembers the session's sandbox, which is what to close later.
func (s *Server) keepSandbox(sessionID string, box sandbox.Sandbox) {
	s.sandboxesMu.Lock()
	defer s.sandboxesMu.Unlock()
	s.sandboxes[sessionID] = box
}

// startFailed says on the session's own row that a dispatch could not start, and hands back the
// error to return.
//
// A row written for a dispatch that then failed used to stay idle in the listing with no task and no
// container behind it, which reads as a session waiting for work rather than one that never began.
// Rows were left that way by the fault in issue 400.
func (s *Server) startFailed(ctx context.Context, session *quaycrewv1.Session, text string, err error) error {
	ctx = context.WithoutCancel(ctx)
	failure := taskFailure(err)
	s.recordTask(ctx, session.GetId(), "", StatusFailed)
	s.recordHistory(ctx, session, &quaycrewv1.TaskEvent{Prompt: text, Status: StatusFailed, Failure: failure})
	s.emit(ctx, session, KindSessionErrored, failure)
	return err
}

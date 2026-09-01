package controlplane

import (
	"context"
	"strings"
	"sync"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StatusTaskStopped is what a task's record says when somebody stopped it.
//
// It is beside "failed" rather than folded into it. An operator asking for a stop is not a fault, and
// a stop that reports as a crash hides the crashes: two sessions were killed by hand on 27 August
// 2026 and both landed as "model: run exited: signal: killed, and it said nothing about why", which
// is indistinguishable from a container the daemon took away.
const StatusTaskStopped = "stopped"

// stopWait is how long StopTask waits for the task to actually end before saying it could not
// confirm the stop.
//
// The command answers only when the task has stopped, because an operator who reads success stops
// watching. So this is a ceiling on a failure rather than an expected wait: what it has to outlast is
// the model runtime noticing its context is cancelled and the landing being written, which is one
// process teardown and one store write.
//
// Sized against the budget the system already gives that shape of work. tidyWait is what tearing a half
// made container down is given, on the reasoning that it is one call to a daemon that ran a container
// end to end in about two seconds, and this is three of those. Provisional, and what replaces it is
// the distribution of the gap between a stop being asked for and the task record closing, over the
// first fifty stops.
const stopWait = 3 * tidyWait

// running is a task in flight, and how to stop it.
//
// The cancel function is the whole mechanism: a task runs the model through a context, so cancelling
// it is what a stop actually is. The reason travels beside it so the landing can be recorded as a
// stop with the operator's own words rather than as whatever the runtime said about being killed.
type running struct {
	cancel context.CancelFunc
	// done is closed once the task has landed, which is what lets a stop answer only when the task
	// has really ended rather than when it was asked to.
	done chan struct{}
	mu   sync.Mutex
	// reason is what the operator said. Read under the lock because the task goroutine reads it as it
	// lands while the stopping call writes it.
	reason string
	asked  bool
}

// stopping records what somebody asked, and says whether this is the first ask. A second stop on a
// task already stopping is not an error and does not overwrite the first reason: the first is the one
// that ended it.
func (r *running) stopping(reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.asked {
		return false
	}
	r.asked, r.reason = true, reason
	return true
}

// stopped says whether somebody asked for this task to stop, and what they said.
func (r *running) stopped() (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.asked, r.reason
}

// beginRunning registers a task as the session's one in flight and hands back the context it runs
// under, so a stop has something to cancel.
//
// One task per session, which is what the system already enforces everywhere else: a dispatch to a
// session continues its conversation, and two tasks in one conversation at once is not a thing the
// model runtime does. A second registration replaces the first rather than being refused, because
// refusing here would fail a task over bookkeeping.
func (s *Server) beginRunning(ctx context.Context, sessionID string) (context.Context, *running) {
	inflight, cancel := context.WithCancel(ctx)
	held := &running{cancel: cancel, done: make(chan struct{})}
	s.runningMu.Lock()
	if s.running == nil {
		s.running = map[string]*running{}
	}
	s.running[sessionID] = held
	s.runningMu.Unlock()
	return inflight, held
}

// endRunning forgets the session's task and releases the context, whatever the task came to.
func (s *Server) endRunning(sessionID string, held *running) {
	s.runningMu.Lock()
	// Only if it is still this task's registration. A dispatch that arrived while this one was
	// landing owns the slot now, and clearing it would leave the system unable to stop that one.
	if s.running[sessionID] == held {
		delete(s.running, sessionID)
	}
	s.runningMu.Unlock()
	close(held.done)
	held.cancel()
}

// runningIn is the task a session has in flight, if it has one.
func (s *Server) runningIn(sessionID string) (*running, bool) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	held, inflight := s.running[sessionID]
	return held, inflight
}

// StopTask halts the task a session is running and keeps the session.
//
// The session survives on purpose. Its conversation, its container and its history all stay, so the
// next dispatch continues the same conversation: what the operator wanted stopped is the task, not
// the room it was working in. StopSession is the other command, and it puts the whole session down.
//
// It answers only once the task has actually ended. Killing the dispatch client was what people
// reached for before this existed, and it is not an interface: on 27 August 2026 the same kill ended
// one task at once and left another working for sixteen more minutes, merging two pull requests after
// the operator believed it had stopped.
//
// A stop while nothing is running says so and changes nothing.
func (s *Server) StopTask(ctx context.Context, req *quaycrewv1.StopTaskRequest) (
	*quaycrewv1.StopTaskResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	held, inflight := s.runningIn(session.GetId())
	if !inflight {
		return &quaycrewv1.StopTaskResponse{Stopped: false, Session: session}, nil
	}
	if held.stopping(strings.TrimSpace(req.GetReason())) {
		s.emit(ctx, session, KindSessionHalted, stopDetail(req.GetReason()))
	}
	held.cancel()

	waiting, giveUp := context.WithTimeout(ctx, stopWait)
	defer giveUp()
	select {
	case <-held.done:
		return &quaycrewv1.StopTaskResponse{Stopped: true, Session: s.reread(ctx, session.GetId())}, nil
	case <-waiting.Done():
		// Said rather than reported as done. An operator who reads success stops watching, so a stop
		// the system could not confirm has to come back as one it could not confirm.
		return nil, status.Errorf(codes.DeadlineExceeded,
			"session %s was asked to stop and its task has not ended yet: watch it with krewe task list %s",
			display.ShortID(session.GetHandle()), display.ShortID(session.GetHandle()))
	}
}

// landStopped closes the record of a task somebody stopped, and hands back the error to return.
//
// The session comes back idle rather than failed, because it is: nothing is running in it, its
// conversation is intact, and the next dispatch continues it. The task carries "stopped" and the
// operator's own reason, so a listing tells a stop from a crash at a glance.
//
// Every write here takes a detached context. The context this task ran under is the one the stop just
// cancelled, and a record written through it would not land, which would leave the task reading as
// still running forever.
func (s *Server) landStopped(ctx context.Context, session *quaycrewv1.Session,
	task *quaycrewv1.TaskEvent, spent model.Response, reason string) error {
	landing := context.WithoutCancel(ctx)
	s.recordTask(landing, session.GetId(), "", StatusIdle)
	// A stopped task still spent what it spent up to the moment it was stopped, and a bill that
	// counts only the tasks that ran to the end understates itself.
	s.measureTask(landing, session, spent, StatusTaskStopped)
	s.landTask(landing, session, task, StatusTaskStopped, "", stopDetail(reason))
	return status.Errorf(codes.Canceled, "the task in session %s was stopped: %s",
		display.ShortID(session.GetHandle()), stopDetail(reason))
}

// stopDetail is what the record of a stop says, when the operator gave no words of their own.
func stopDetail(reason string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}
	return "stopped by the operator"
}

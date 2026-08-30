package controlplane

import (
	"context"
	"log/slog"
	"sync"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/store"
)

// nameConversation gives a session the name of the conversation its tasks run in, and keeps it.
//
// Before the task rather than after it, which is the whole point. The system used to pass no name at
// all on a session's first task, read the name the runtime chose out of the output stream, and record
// it once the task had landed. So for the whole life of that first task the system held no name, and
// opening the session meanwhile found an empty field, named a second conversation and opened that
// one. The operator watched an empty conversation while the job happened in another.
//
// Called again on a session that has one, this does nothing: a conversation is named once and the
// name is the only pointer to the transcript.
func (s *Server) nameConversation(ctx context.Context, session *quaycrewv1.Session) string {
	if held := session.GetModelSessionId(); held != "" {
		return held
	}
	named := store.NewConversationID()
	if err := s.store.RecordTask(ctx, session.GetId(), named, session.GetStatus()); err != nil {
		// The task still runs, under a name nothing else can see. Worth a line, and not worth refusing
		// the job over: a session that cannot be opened is better than a session that cannot work.
		slog.WarnContext(ctx, "the conversation could not be named", "session", session.GetId(), "error", err)
	}
	// Carried on the session whether the write landed or not, so everything downstream of this call
	// uses one name. Leaving it off after a failed write means the next caller mints a second name and
	// the task and the session disagree about which conversation this is.
	session.ModelSessionId = named
	return named
}

// conversationStarted says whether the model runtime has opened this conversation already, which is
// what decides whether the next task starts it or resumes it.
//
// Two sources, and they answer for different systems. The transcript on the host is the truth wherever
// the system keeps state there, and it is the same file the sandbox script reads when an operator opens
// a conversation by hand, so a conversation somebody typed in is known to have started. A system that
// keeps nothing on the host can see no transcript at all, and for that one the system's own memory of
// the runtime having reported the name back is all there is.
func (s *Server) conversationStarted(session *quaycrewv1.Session, name string) bool {
	if name == "" {
		return false
	}
	if s.storage.HasConversation(boxOf(session), name) {
		return true
	}
	return s.opened.holds(name)
}

// confirmConversation checks what the runtime called the conversation against the name the system gave
// it, and returns the name to record against the session.
//
// The identifier in the output stream is a check now rather than the source. A runtime that was given
// a name and used it confirms the system's bookkeeping; one that used a different name ignored the flag,
// and everything the system reports for that session afterwards is read from a transcript nobody wrote,
// so it says so with both names in the line.
//
// It returns the reported name only for a session the system never named, which is a session whose task
// started before this system did. Recording nothing leaves the stored name alone.
func (s *Server) confirmConversation(ctx context.Context, session *quaycrewv1.Session, reported string) string {
	asked := session.GetModelSessionId()
	if asked == "" {
		s.opened.add(reported)
		return reported
	}
	if said := model.ConversationCheck(asked, reported); said != "" {
		slog.WarnContext(ctx, said, "session", session.GetId(), "asked", asked, "used", reported)
		return ""
	}
	if reported != "" {
		s.opened.add(asked)
	}
	return ""
}

// opened is the conversations this process has watched a model runtime open, which is how a system that
// keeps no state on the host still knows a second task must resume rather than start. It is in memory
// on purpose: a system that keeps state on the host reads the transcript instead, and that survives a
// restart, where this does not.
type opened struct {
	mu    sync.Mutex
	names map[string]struct{}
}

func (o *opened) add(name string) {
	if name == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.names == nil {
		o.names = map[string]struct{}{}
	}
	o.names[name] = struct{}{}
}

func (o *opened) holds(name string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, found := o.names[name]
	return found
}

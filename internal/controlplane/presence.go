package controlplane

import (
	"context"
	"log/slog"
	"sync"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
)

// presenceSweep is the whole listing's budget for asking the sandboxes what is in them.
//
// A listing is drawn while somebody waits for it, so a daemon that has stopped answering has to cost
// the view a moment rather than the whole view, and the cost must not grow with the number of rows:
// one budget for the sweep rather than one per session is what holds that. A row still waiting when
// it runs out reads untold, never empty, because the point of the field is that the system says when
// it does not know.
const presenceSweep = 5 * time.Second

// presenceAtOnce is how many sandboxes are asked at the same time.
//
// The questions are network round trips to the daemon rather than job this process does, so they
// overlap: twenty sessions asked one after another cost twenty round trips end to end, and asked
// eight at a time they cost three. The figure is a ceiling on how much of the daemon one listing may
// take, because the same daemon is running every session on the machine.
const presenceAtOnce = 8

// withPresence reads what is inside each session's sandbox and puts it on the row.
//
// Only the sessions that would otherwise read idle are asked. Every other word already says
// something is happening or that the container has gone, so asking would cost a round trip to learn
// nothing: a system where nothing is idle pays nothing here.
//
// Nothing is created to answer. The provider is asked by name, because the sandbox handles are a map
// in one process and the containers are not, so a question that built a sandbox would start the very
// container it is asked about taking away.
func (s *Server) withPresence(ctx context.Context, sessions []*quaycrewv1.Session) {
	asking := make([]*quaycrewv1.Session, 0, len(sessions))
	for _, session := range sessions {
		if session.GetStatus() == StatusIdle && session.GetArchivedAt() == nil {
			asking = append(asking, session)
		}
	}
	if len(asking) == 0 {
		return
	}

	// The whole sweep, not each question, because a listing is drawn while somebody waits for it and
	// a system of forty rows must not cost twice what a system of twenty costs when the daemon is wedged.
	ctx, cancel := context.WithTimeout(ctx, presenceSweep)
	defer cancel()

	slots := make(chan struct{}, presenceAtOnce)
	var waiting sync.WaitGroup
	for _, session := range asking {
		waiting.Add(1)
		go func(session *quaycrewv1.Session) {
			defer waiting.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			session.Presence = s.presenceOf(ctx, session.GetId())
		}(session)
	}
	waiting.Wait()
}

// presenceOf asks one session's sandbox what is in it.
//
// Both questions are asked, and they are asked together rather than one after the other, because
// neither answer stands for the other. Somebody sitting in a conversation they have closed is
// attached with no runtime up, and a conversation answering after they detached is a runtime up with
// nobody attached. Reading one and inferring the other reports one of those two as an empty
// container.
//
// Attached wins where both are true, because it is the stronger claim about who the container
// belongs to: a person is in there.
//
// Either question failing is untold, which is the whole reason the value exists. The provider
// returns an error when it cannot reach the daemon at all, and the system must not turn that into
// "nothing is there": a caller reads that as licence to take the container.
func (s *Server) presenceOf(ctx context.Context, sessionID string) quaycrewv1.SessionPresence {
	var attached, awake bool
	var attachErr, runtimeErr error
	var asking sync.WaitGroup
	asking.Add(2)
	go func() {
		defer asking.Done()
		attached, attachErr = s.provider.Attached(ctx, sessionID)
	}()
	go func() {
		defer asking.Done()
		awake, runtimeErr = s.provider.RuntimeRunning(ctx, sessionID)
	}()
	asking.Wait()

	switch {
	case attachErr != nil || runtimeErr != nil:
		slog.DebugContext(ctx, "a session's sandbox did not say what is in it",
			"session", sessionID, "attached", attachErr, "runtime", runtimeErr)
		return quaycrewv1.SessionPresence_SESSION_PRESENCE_UNTOLD
	case attached:
		return quaycrewv1.SessionPresence_SESSION_PRESENCE_ATTACHED
	case awake:
		return quaycrewv1.SessionPresence_SESSION_PRESENCE_AWAKE
	default:
		return quaycrewv1.SessionPresence_SESSION_PRESENCE_EMPTY
	}
}

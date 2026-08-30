package controlplane_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Reclaiming, and the signal that stops it closing a container somebody is in.

// aSystemWithProvider is a control plane over a provider a test can drive, so a scenario can be a
// daemon that says somebody is attached, or one that will not answer at all.
func aSystemWithProvider(runner model.Runner, provider *sandbox.FakeProvider) *controlplane.Server {
	return controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: provider, Secrets: secrets.NewMemory(),
	})
}

// anIdleSession dispatches once and waits for the answer, which leaves a session with a container and
// a conversation, idle.
func anIdleSession(t *testing.T, s *controlplane.Server, project string) *quaycrewv1.Session {
	t.Helper()
	sent, err := s.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	session, err := s.GetSession(context.Background(), &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return session.GetSession()
}

func TestReclaimingTakesTheContainerAndKeepsEverythingElse(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)
	named := session.GetModelSessionId()
	if named == "" {
		t.Fatal("the session holds no conversation, so there is nothing for a reclaim to keep")
	}

	reclaimed, err := s.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: session.GetId()})
	if err != nil {
		t.Fatalf("ReclaimSession: %v", err)
	}

	if reclaimed.GetSession().GetStatus() != controlplane.StatusReclaimed {
		t.Fatalf("the session reads %q, want reclaimed", reclaimed.GetSession().GetStatus())
	}
	if reclaimed.GetSession().GetModelSessionId() != named {
		t.Fatalf("the conversation handle reads %q, and a reclaim must keep it: it is the only "+
			"pointer to the transcript the next container resumes",
			reclaimed.GetSession().GetModelSessionId())
	}
	if !holdsString(provider.Removed, session.GetId()) {
		t.Fatalf("the provider was asked to remove %v, and the reclaimed session is not among them",
			provider.Removed)
	}
}

// The whole promise of reclaimed: a task sent to one starts a new container and carries on.
func TestATaskSentToAReclaimedSessionStartsAFreshContainerAndKeepsTheHistory(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	runner := &model.FakeRunner{Reply: "first"}
	s := aSystemWithProvider(runner, provider)
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)
	made := len(provider.Configurations())

	if _, err := s.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: session.GetId()}); err != nil {
		t.Fatalf("ReclaimSession: %v", err)
	}
	runner.Reply = "second"
	again, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Handle: session.GetHandle(), Text: "still there?",
	})
	if err != nil {
		t.Fatalf("dispatching to a reclaimed session: %v", err)
	}

	if again.GetReply() != "second" {
		t.Fatalf("the reclaimed session answered %q, want the new task's answer", again.GetReply())
	}
	if again.GetId() != session.GetId() {
		t.Fatalf("the task landed in session %s, want the same one it was reclaimed from %s",
			again.GetId(), session.GetId())
	}
	if len(provider.Configurations()) != made+1 {
		t.Fatalf("%d containers were built in all, want one more than the %d before the reclaim",
			len(provider.Configurations()), made)
	}
	// The history is the point. A reclaim that lost it would be a stop with a friendlier word.
	tasks, err := s.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session.GetId()})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.GetTasks()) != 2 {
		t.Fatalf("the session holds %d tasks after the reclaim, want both of them", len(tasks.GetTasks()))
	}
	if tasks.GetTasks()[0].GetPrompt() != "hello" {
		t.Fatalf("the first task reads %q, so the history from before the reclaim is gone",
			tasks.GetTasks()[0].GetPrompt())
	}
	back, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: session.GetId()})
	if back.GetSession().GetReclaimedAt() != nil {
		t.Fatal("the session still carries a reclaim stamp after a task ran in it")
	}
}

func TestASessionWithATaskUnderWayIsNotReclaimed(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{})
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done", Gate: gate, Started: started}, provider)
	_, project := newProject(t, s)
	ctx := context.Background()

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "take your time", Detach: true})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	<-started

	_, err = s.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: sent.GetId()})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reclaiming a working session answered %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "task under way") {
		t.Fatalf("the refusal reads %q, and it has to say a task is running", err)
	}
	close(gate)
	s.WaitForTasks(ctx)
}

func TestAStoppedSessionIsNotReclaimed(t *testing.T) {
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, &sandbox.FakeProvider{})
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)
	if _, err := s.StopSession(ctx, &quaycrewv1.StopSessionRequest{Id: session.GetId()}); err != nil {
		t.Fatalf("StopSession: %v", err)
	}

	_, err := s.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: session.GetId()})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reclaiming a stopped session answered %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "stop is the operator's") {
		t.Fatalf("the refusal reads %q, and it has to say a stop belongs to the operator", err)
	}
}

func TestReclaimingTwiceIsRefusedRatherThanRestamped(t *testing.T) {
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, &sandbox.FakeProvider{})
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)
	if _, err := s.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: session.GetId()}); err != nil {
		t.Fatalf("ReclaimSession: %v", err)
	}
	first, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: session.GetId()})

	_, err := s.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: session.GetId()})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reclaiming twice answered %v, want a refusal", err)
	}
	again, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: session.GetId()})
	if !again.GetSession().GetReclaimedAt().AsTime().Equal(first.GetSession().GetReclaimedAt().AsTime()) {
		t.Fatal("the second reclaim moved the stamp, which would hold the session out of the archive forever")
	}
}

func TestAnArchivedSessionIsNotReclaimed(t *testing.T) {
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, &sandbox.FakeProvider{})
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)
	if _, err := s.ArchiveSession(ctx, &quaycrewv1.ArchiveSessionRequest{Id: session.GetId()}); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	_, err := s.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: session.GetId()})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reclaiming an archived session answered %v, want a refusal", err)
	}
}

func TestReclaimingASessionNobodyHasIsNotFound(t *testing.T) {
	s := aSystemWithProvider(&model.FakeRunner{}, &sandbox.FakeProvider{})
	_, err := s.ReclaimSession(context.Background(), &quaycrewv1.ReclaimSessionRequest{Id: "ghost"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("reclaiming a missing session answered %v, want NotFound", err)
	}
}

// The attached signal, read the way the controller reads it.
func TestTheSystemCanTellWhoIsInASession(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)

	alone, err := s.SessionAttached(ctx, session.GetId())
	if err != nil {
		t.Fatalf("SessionAttached: %v", err)
	}
	if alone {
		t.Fatal("the system says somebody is in a session nobody has opened")
	}

	provider.Watch(session.GetId())
	watched, err := s.SessionAttached(ctx, session.GetId())
	if err != nil {
		t.Fatalf("SessionAttached: %v", err)
	}
	if !watched {
		t.Fatal("the system says nobody is in a session an operator has open")
	}
}

// A daemon that will not answer is the system being unable to tell, and it must come back as an error
// rather than as nobody: the controller reads an error as attached and leaves the container alone.
func TestADaemonThatWillNotAnswerIsNotTheSameAsNobody(t *testing.T) {
	provider := &sandbox.FakeProvider{AttachErr: errors.New("the daemon is not there")}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)

	if _, err := s.SessionAttached(ctx, session.GetId()); err == nil {
		t.Fatal("a daemon that could not be asked answered that nobody is attached")
	}
}

// A session with no container is nobody's to be in, and the system must not build one to find out.
//
// The session is one somebody was in before its container went, which is the case that tells the
// answer apart from a lookup of who was watching: the container is what is asked, so once it has gone
// the answer is nobody whatever anybody was doing a moment earlier.
func TestASessionWithNoContainerIsNobodyIsIn(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)
	provider.Watch(session.GetId())
	if _, err := s.ReclaimSession(ctx, &quaycrewv1.ReclaimSessionRequest{Id: session.GetId()}); err != nil {
		t.Fatalf("ReclaimSession: %v", err)
	}
	made := len(provider.Configurations())

	watched, err := s.SessionAttached(ctx, session.GetId())
	if err != nil {
		t.Fatalf("SessionAttached: %v", err)
	}
	if watched {
		t.Fatal("the system says somebody is in a session whose container it took back")
	}
	if len(provider.Configurations()) != made {
		t.Fatal("asking who is in a session built a container, which would undo the reclaim it is asked about")
	}
}

func holdsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

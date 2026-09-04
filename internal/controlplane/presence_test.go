package controlplane_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// What a listing says is inside a session's sandbox. The row's status only ever said whether a
// dispatched exec was open, so a conversation somebody opened and left answering read idle, and idle
// is the word that invites a restart, a drain or a reclaim.

// listOne lists the system's sessions, asking what is in each sandbox, and returns the only one.
func listOne(t *testing.T, s *controlplane.Server) *quaycrewv1.Session {
	t.Helper()
	listed, err := s.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{Presence: true})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.GetSessions()) != 1 {
		t.Fatalf("the system has %d sessions, so there is no single one to read",
			len(listed.GetSessions()))
	}
	return listed.GetSessions()[0]
}

// TestASessionWhoseRuntimeIsRunningDoesNotReadIdle is the rule this whole slice ships on. Six of
// eighteen sandboxes were measured holding a running runtime on 28 August 2026, and every one of them
// listed as idle, which is what makes a drain and a reclaim dangerous: both act on the listing.
func TestASessionWhoseRuntimeIsRunningDoesNotReadIdle(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	session := anIdleSession(t, s, project)

	// A conversation answering with nobody watching it: the state nothing in the system could see.
	provider.Wake(session.GetId())

	listed := listOne(t, s)
	if listed.GetPresence() != quaycrewv1.SessionPresence_SESSION_PRESENCE_AWAKE {
		t.Fatalf("the system read the sandbox as %v, and a runtime is running in it",
			listed.GetPresence())
	}
	if word := display.SessionStatus(listed); word == display.StatusIdle {
		t.Fatal("a session holding a running conversation reads idle, which is the whole defect: " +
			"an operator reading that word restarts, drains or reclaims over the top of it")
	} else if word != display.StatusAwake {
		t.Fatalf("the listing says %q, want %q", word, display.StatusAwake)
	}
	// The row itself is untouched. Status says whether a dispatched exec is open and that is still
	// true, so an event log and a job controller reading it are unaffected.
	if listed.GetStatus() != controlplane.StatusIdle {
		t.Fatalf("the session's own status reads %q, and no exec is open, so it should read idle",
			listed.GetStatus())
	}
}

// TestSomebodyTypingReadsAsAttached. Awake and attached are both "leave it alone", and they are still
// worth telling apart: one is a person at a keyboard and the other is a conversation to go back to.
func TestSomebodyTypingReadsAsAttached(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	session := anIdleSession(t, s, project)

	// Somebody is in there and the runtime is up, which is what an open conversation looks like.
	provider.Watch(session.GetId())
	provider.Wake(session.GetId())

	if word := display.SessionStatus(listOne(t, s)); word != display.StatusAttached {
		t.Fatalf("the listing says %q, want %q: a person at a keyboard is the stronger claim on a "+
			"container than a runtime nobody is watching", word, display.StatusAttached)
	}
}

// TestAConversationSomebodyClosedButIsStillSittingInReadsAttached. The case that stops the two
// questions being folded into one. Ending a conversation leaves the terminal alive at a prompt, so
// the runtime is gone while somebody is still attached, and asking only about the runtime would call
// that container empty.
func TestAConversationSomebodyClosedButIsStillSittingInReadsAttached(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	session := anIdleSession(t, s, project)

	provider.Watch(session.GetId())

	if word := display.SessionStatus(listOne(t, s)); word != display.StatusAttached {
		t.Fatalf("the listing says %q, want %q", word, display.StatusAttached)
	}
}

// TestAnEmptySandboxIsTheOnlyRealIdle. The other half of the rule. Without this the reclaim can never
// take a container back, and the system holds every container it ever made.
func TestAnEmptySandboxIsTheOnlyRealIdle(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	anIdleSession(t, s, project)

	listed := listOne(t, s)
	if listed.GetPresence() != quaycrewv1.SessionPresence_SESSION_PRESENCE_EMPTY {
		t.Fatalf("an empty sandbox read as %v", listed.GetPresence())
	}
	if word := display.SessionStatus(listed); word != display.StatusIdle {
		t.Fatalf("the listing says %q for a sandbox with nothing in it, want %q",
			word, display.StatusIdle)
	}
}

// TestADaemonThatWillNotAnswerIsNeverIdle. The sad path that matters most. A provider that cannot
// tell returns an error, and a system that turned that into "nothing is there" would hand an operator
// the exact word that invites them to take the container.
func TestADaemonThatWillNotAnswerIsNeverIdle(t *testing.T) {
	for name, breaking := range map[string]func(p *sandbox.FakeProvider){
		"the daemon cannot say what is running": func(p *sandbox.FakeProvider) {
			p.RuntimeErr = errors.New("cannot connect to the docker daemon")
		},
		"the daemon cannot say who is attached": func(p *sandbox.FakeProvider) {
			p.AttachErr = errors.New("cannot connect to the docker daemon")
		},
	} {
		t.Run(name, func(t *testing.T) {
			provider := &sandbox.FakeProvider{}
			s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
			_, project := newProject(t, s)
			anIdleSession(t, s, project)
			breaking(provider)

			listed := listOne(t, s)
			if listed.GetPresence() != quaycrewv1.SessionPresence_SESSION_PRESENCE_UNTOLD {
				t.Fatalf("the system could not tell and reported %v", listed.GetPresence())
			}
			if word := display.SessionStatus(listed); word == display.StatusIdle {
				t.Fatal("the system could not reach the daemon and reported the session as idle, " +
					"which reads as licence to take its container")
			} else if word != display.StatusUnknown {
				t.Fatalf("the listing says %q, want %q", word, display.StatusUnknown)
			}
		})
	}
}

// TestASessionWithNoContainerIsIdleRatherThanUnknown. The other sad path, and the one that must not
// be answered with unknown: a session whose sandbox was never made, or has gone, is genuinely empty.
// The provider says so without failing, the way the real one reads a non zero exit from a container
// that is not there.
func TestASessionWithNoContainerIsIdleRatherThanUnknown(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	_, project := newProject(t, s)
	session := anIdleSession(t, s, project)

	if _, err := s.ReclaimSession(context.Background(),
		&quaycrewv1.ReclaimSessionRequest{Id: session.GetId()}); err != nil {
		t.Fatalf("ReclaimSession: %v", err)
	}

	// A reclaimed session keeps its own word, because reclaimed says something presence cannot: the
	// container has gone and the conversation has not.
	listed := listOne(t, s)
	if word := display.SessionStatus(listed); word != controlplane.StatusReclaimed {
		t.Fatalf("the listing says %q for a reclaimed session, want reclaimed", word)
	}
}

// TestNothingIsAskedAboutASessionThatIsNotIdle. The cost rule. Every other word already says
// something is happening or that the container has gone, so asking would spend a round trip to the
// daemon per row to learn nothing, once per listing, on a console that redraws every three seconds.
func TestNothingIsAskedAboutASessionThatIsNotIdle(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	provider := &countingProvider{FakeProvider: &sandbox.FakeProvider{}}
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: provider, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, s)
	session := anIdleSession(t, s, project)

	if _, err := s.StopSession(context.Background(),
		&quaycrewv1.StopSessionRequest{Id: session.GetId()}); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	provider.reset()

	if _, err := s.ListSessions(context.Background(),
		&quaycrewv1.ListSessionsRequest{Presence: true}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if asked := provider.asked(); asked != 0 {
		t.Fatalf("a stopped session's sandbox was asked %d questions, and it has no container", asked)
	}
}

// TestAListingThatDidNotAskLeavesTheRowAlone. A caller that does not ask pays nothing and reads
// exactly what it read before, which is what the machinery resolving an address or finding a session
// by name wants.
func TestAListingThatDidNotAskLeavesTheRowAlone(t *testing.T) {
	provider := &countingProvider{FakeProvider: &sandbox.FakeProvider{}}
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "done"},
		Provider: provider, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, s)
	session := anIdleSession(t, s, project)
	provider.Wake(session.GetId())
	provider.reset()

	listed, err := s.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if asked := provider.asked(); asked != 0 {
		t.Fatalf("nobody asked for presence and the system put %d questions to the daemon", asked)
	}
	if got := listed.GetSessions()[0].GetPresence(); got != quaycrewv1.SessionPresence_SESSION_PRESENCE_UNSPECIFIED {
		t.Fatalf("presence reads %v on a listing that did not ask for it", got)
	}
	if word := display.SessionStatus(listed.GetSessions()[0]); word != display.StatusIdle {
		t.Fatalf("a row nobody asked about reads %q, and it should read what it always read", word)
	}
}

// countingProvider counts the questions a listing puts to the daemon, so a test can hold the cost
// rule: one question per row that would otherwise read idle, and none for any other row.
type countingProvider struct {
	*sandbox.FakeProvider
	mu        sync.Mutex
	questions int
}

func (p *countingProvider) Attached(ctx context.Context, sessionID string) (bool, error) {
	p.count()
	return p.FakeProvider.Attached(ctx, sessionID)
}

func (p *countingProvider) RuntimeRunning(ctx context.Context, sessionID string) (bool, error) {
	p.count()
	return p.FakeProvider.RuntimeRunning(ctx, sessionID)
}

func (p *countingProvider) count() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.questions++
}

func (p *countingProvider) asked() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.questions
}

func (p *countingProvider) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.questions = 0
}

// TestASessionTheSystemDoesNotHaveIsAnErrorRatherThanAnIdleRow. The sad path that must never be
// answered with a word at all. Nobody is in a session that does not exist, and a caller reads idle as
// licence to close a container, so it must not get that answer from a lookup that failed.
func TestASessionTheSystemDoesNotHaveIsAnErrorRatherThanAnIdleRow(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, provider)
	newProject(t, s)
	ctx := context.Background()

	if _, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: "a-session-nobody-made"}); err == nil {
		t.Fatal("the system answered for a session it does not have")
	}
	if _, err := s.SessionAttached(ctx, "a-session-nobody-made"); err == nil {
		t.Fatal("the system said whether somebody is attached to a session it does not have")
	}
	// And a listing narrowed to a workspace that is not there is empty rather than a row of idles.
	listed, err := s.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{
		Workspace: "a-workspace-nobody-made", Presence: true,
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.GetSessions()) != 0 {
		t.Fatalf("the system listed %d sessions for a workspace it does not have",
			len(listed.GetSessions()))
	}
}

// hangingProvider never answers, which is a daemon that has taken the request and gone quiet. It is
// worse than one that refuses: nothing comes back at all, and a listing that waited on it would hold
// the console open for as long as the daemon stayed wedged.
type hangingProvider struct{ *sandbox.FakeProvider }

func (hangingProvider) Attached(ctx context.Context, _ string) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

func (hangingProvider) RuntimeRunning(ctx context.Context, _ string) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

// TestADaemonThatNeverAnswersStillGivesTheOperatorAListing. The sad path with no error in it, and the
// one that would freeze a console: the daemon takes the request and goes quiet.
//
// More rows than the sweep asks at once, on purpose. A budget spent per session rather than per
// listing would hold this system for four waves, so the number of sessions has to be large enough that
// the two answers are different lengths of time apart.
func TestADaemonThatNeverAnswersStillGivesTheOperatorAListing(t *testing.T) {
	quiet := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "done"},
		Provider: hangingProvider{&sandbox.FakeProvider{}}, Secrets: secrets.NewMemory(),
	})
	_, project := newProject(t, quiet)
	const rows = 25
	for range rows {
		anIdleSession(t, quiet, project)
	}

	began := time.Now()
	listed, err := quiet.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{Presence: true})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	took := time.Since(began)

	if len(listed.GetSessions()) != rows {
		t.Fatalf("the listing carries %d rows, want %d", len(listed.GetSessions()), rows)
	}
	for _, session := range listed.GetSessions() {
		if word := display.SessionStatus(session); word != display.StatusUnknown {
			t.Fatalf("a session whose sandbox never answered reads %q, want %q",
				word, display.StatusUnknown)
		}
	}
	// A bound rather than a measurement, and it is the whole listing's rather than one row's: the
	// budget is five seconds, four waves of it would be twenty, and a loaded machine is allowed the
	// room in between.
	if took > 8*time.Second {
		t.Fatalf("a listing of %d rows took %s to give up on a daemon that never answers, so the "+
			"cost of a wedged daemon grows with the number of sessions", rows, took)
	}
}

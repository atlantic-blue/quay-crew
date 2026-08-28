package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// What a caller reads out of the crew has to be the value and nothing else, because the next thing
// that happens to it is another command. The history listing is the other rendering of the same
// records, written for a person: it shortens a reply at 120 characters and puts a clock beside it.

// errTheModelRefused is what a task that did not land failed with.
var errTheModelRefused = errors.New("the model refused this task")

// countingRunner answers something different for each task, so a test about order can name the
// answer it wants rather than counting on which prompt came first. From holdFrom onwards a task is
// held open, which is how a test gets a session with an answer behind it and a task still running.
type countingRunner struct {
	mu       sync.Mutex
	count    int
	holdFrom int
	gate     chan struct{}
	started  chan struct{}
	once     sync.Once
}

var _ model.Runner = (*countingRunner)(nil)

func (c *countingRunner) Run(ctx context.Context, _ sandbox.Sandbox, _ model.Request) (model.Response, error) {
	c.mu.Lock()
	c.count++
	which := c.count
	c.mu.Unlock()

	if c.holdFrom > 0 && which >= c.holdFrom {
		if c.started != nil {
			c.once.Do(func() { close(c.started) })
		}
		select {
		case <-c.gate:
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
	}
	return model.Response{
		Reply:          fmt.Sprintf("answer %d", which),
		ModelSessionID: fmt.Sprintf("conversation-%d", which),
	}, nil
}

// aCrewRunning stands up a crew with one workspace and one project behind the given model.
func aCrewRunning(t *testing.T, runner model.Runner) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client
}

// aSessionThatAnswered is a crew with one session whose only task landed.
func aSessionThatAnswered(t *testing.T, reply string) (quaycrewv1.ControlPlaneServiceClient, *quaycrewv1.Session) {
	t.Helper()
	client := aCrewRunning(t, &model.FakeRunner{Reply: reply})
	mustRun(t, client, "task", "when is the electricity bill due")
	return client, onlySession(t, client)
}

// aDriverSession is a session the crew opened and nobody has dispatched to, which is how a session
// exists with an empty history.
func aDriverSession(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) *quaycrewv1.Session {
	t.Helper()
	listed, err := client.ListProjects(context.Background(), &quaycrewv1.ListProjectsRequest{})
	if err != nil || len(listed.GetProjects()) != 1 {
		t.Fatalf("want exactly one project, got %d (%v)", len(listed.GetProjects()), err)
	}
	opened, err := client.OpenDriver(context.Background(), &quaycrewv1.OpenDriverRequest{
		Project: listed.GetProjects()[0].GetId(),
	})
	if err != nil {
		t.Fatalf("open the driver: %v", err)
	}
	return opened.GetSession()
}

// held is a session whose task the model is still working on, and the func that lets it finish.
type held struct {
	session string
	release func()
}

// aSessionHoldingATask holds the task open rather than timing it: what is under test is what is true
// while a task runs, and a test that waits a duration for that passes on a fast machine by accident.
func aSessionHoldingATask(t *testing.T, after int) (quaycrewv1.ControlPlaneServiceClient, held) {
	t.Helper()
	runner := &countingRunner{
		holdFrom: after + 1,
		gate:     make(chan struct{}), started: make(chan struct{}),
	}
	client := aCrewRunning(t, runner)

	session := ""
	for landed := 0; landed < after; landed++ {
		if session == "" {
			mustRun(t, client, "task", "first")
			session = onlySession(t, client).GetHandle()
			continue
		}
		mustRun(t, client, "task", "me/house-bills/"+session[:8], "again")
	}
	if session == "" {
		mustRun(t, client, "task", flagDispatch, "read the repository")
	} else {
		mustRun(t, client, "task", flagDispatch, "me/house-bills/"+session[:8], "read the repository")
	}
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}
	return client, held{
		session: onlySession(t, client).GetId(),
		release: sync.OnceFunc(func() { close(runner.gate) }),
	}
}

// The whole of the command: what a caller reads is the reply, and nothing else is on the stream.
func TestTheAnswerIsTheReplyAndOneNewline(t *testing.T) {
	client, session := aSessionThatAnswered(t, "the 14th")

	printed, err := runQuay(t, client, "answer", session.GetId())
	if err != nil {
		t.Fatalf("quay answer: %v", err)
	}
	if printed != "the 14th\n" {
		t.Fatalf("standard output is %q, want the reply and one newline", printed)
	}
}

// A reply that already ends in a newline must not gain a second one, or every caller has to trim.
func TestAReplyThatEndsInANewlineGetsNoSecondOne(t *testing.T) {
	client, session := aSessionThatAnswered(t, "the 14th\n")

	printed, err := runQuay(t, client, "answer", session.GetId())
	if err != nil {
		t.Fatalf("quay answer: %v", err)
	}
	if printed != "the 14th\n" {
		t.Fatalf("standard output is %q, want one trailing newline", printed)
	}
}

// The listing shortens a reply at 120 characters. This is the command that must not.
func TestTheAnswerIsNotShortened(t *testing.T) {
	long := strings.Repeat("x", 400)
	client, session := aSessionThatAnswered(t, long)

	printed, err := runQuay(t, client, "answer", session.GetId())
	if err != nil {
		t.Fatalf("quay answer: %v", err)
	}
	if printed != long+"\n" {
		t.Fatalf("standard output is %d characters, want the answer whole", len(printed))
	}
}

// Whatever a listing prints, this command takes: the id, the handle, either of them shortened the
// way the listing shortens them, and an address.
func TestEveryIdentifierOfASessionReachesItsAnswer(t *testing.T) {
	client, session := aSessionThatAnswered(t, "the 14th")

	for _, named := range []string{
		session.GetId(),
		session.GetId()[:8],
		session.GetHandle(),
		session.GetHandle()[:8],
		"me/house-bills/" + session.GetHandle()[:8],
	} {
		printed, err := runQuay(t, client, "answer", named)
		if err != nil {
			t.Errorf("quay answer %s: %v", named, err)
			continue
		}
		if printed != "the 14th\n" {
			t.Errorf("quay answer %s printed %q, want the reply", named, printed)
		}
	}
}

// A caller that pipes this must never be handed a sentence where the value belongs. So a session
// with nothing to answer with prints nothing at all, and says why on the other stream.
func TestASessionWithNoLandedTaskPrintsNothingAndIsRefused(t *testing.T) {
	client := aCrewRunning(t, &model.FakeRunner{Reply: "ok"})
	session := aDriverSession(t, client)

	printed, err := runQuay(t, client, "answer", session.GetId())
	if err == nil {
		t.Fatal("a session with no landed task answered something")
	}
	if printed != "" {
		t.Fatalf("standard output carries %q, and a caller would take that for the answer", printed)
	}
	if !strings.Contains(err.Error(), "no landed task") {
		t.Fatalf("the refusal says %q, want it to say there is no landed task", err)
	}
}

// A task that has not landed has no answer, and the answer of the task before it is not the answer
// to the question the caller is asking.
func TestATaskStillRunningIsRefusedAsRunning(t *testing.T) {
	client, working := aSessionHoldingATask(t, 0)
	defer working.release()

	printed, err := runQuay(t, client, "answer", working.session)
	if err == nil {
		t.Fatal("a task still running answered something")
	}
	if printed != "" {
		t.Fatalf("standard output carries %q while the task is still running", printed)
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Fatalf("the refusal says %q, want it to say the task is still running", err)
	}
}

// The answer of the task before it is not the answer either: a caller asking for the answer to what
// it just dispatched would read the one before as though it were the new one.
func TestATaskStillRunningIsRefusedEvenWhenAnEarlierOneLanded(t *testing.T) {
	client, working := aSessionHoldingATask(t, 1)
	defer working.release()

	printed, err := runQuay(t, client, "answer", working.session)
	if err == nil {
		t.Fatal("the answer of an earlier task was given for a task still running")
	}
	if printed != "" {
		t.Fatalf("standard output carries %q while the task is still running", printed)
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Fatalf("the refusal says %q, want it to say the task is still running", err)
	}
}

// What a task failed with is the answer to what it was asked, so it goes where the answer goes. The
// exit status is what tells a caller it is reading a failure.
func TestAFailedTasksFailureIsTheAnswerAndTheCommandFails(t *testing.T) {
	client := aCrewRunning(t, &model.FakeRunner{Err: errTheModelRefused})
	if _, err := runQuay(t, client, "task", "read the repository"); err == nil {
		t.Fatal("the task was expected to fail and did not")
	}
	session := onlySession(t, client)

	printed, err := runQuay(t, client, "answer", session.GetId())
	if err == nil {
		t.Fatal("a failed task answered as though it had worked")
	}
	if !strings.Contains(printed, errTheModelRefused.Error()) {
		t.Fatalf("standard output is %q, want what the task failed with", printed)
	}
}

// Oldest first, one record per line, so a caller reads them in the order they happened.
func TestEveryAnswerIsPrintedOldestFirst(t *testing.T) {
	client := aCrewRunning(t, &countingRunner{})
	mustRun(t, client, "task", "first")
	session := onlySession(t, client)
	mustRun(t, client, "task", "me/house-bills/"+session.GetHandle()[:8], "second")

	printed, err := runQuay(t, client, "answer", session.GetId(), "--all")
	if err != nil {
		t.Fatalf("quay answer --all: %v", err)
	}
	if printed != "answer 1\nanswer 2\n" {
		t.Fatalf("standard output is %q, want both answers oldest first", printed)
	}
}

// The same refusal as one answer, because a caller reading a stream cannot be given prose either way.
func TestEveryAnswerOfASessionWithNoLandedTaskIsRefused(t *testing.T) {
	client := aCrewRunning(t, &model.FakeRunner{Reply: "ok"})
	session := aDriverSession(t, client)

	printed, err := runQuay(t, client, "answer", session.GetId(), "--all")
	if err == nil {
		t.Fatal("a session with no landed task answered something")
	}
	if printed != "" {
		t.Fatalf("standard output carries %q", printed)
	}
	if !strings.Contains(err.Error(), "no landed task") {
		t.Fatalf("the refusal says %q, want it to say there is no landed task", err)
	}
}

// A task still running has no answer to print, and it does not stop the ones that landed being read.
func TestEveryAnswerLeavesOutTheTaskStillRunning(t *testing.T) {
	client, working := aSessionHoldingATask(t, 1)
	defer working.release()

	printed, err := runQuay(t, client, "answer", working.session, "--all")
	if err != nil {
		t.Fatalf("quay answer --all: %v", err)
	}
	if printed != "answer 1\n" {
		t.Fatalf("standard output is %q, want the one answer that landed", printed)
	}
}

// The command needs a session, and saying which one is the caller's to do: there is no sensible
// guess, and answering another session's task is worse than refusing.
func TestAnswerWithoutASessionSaysHowItIsCalled(t *testing.T) {
	client := testClient(t)

	printed, err := runQuay(t, client, "answer")
	if err == nil {
		t.Fatal("answer with no session was accepted")
	}
	if printed != "" {
		t.Fatalf("standard output carries %q", printed)
	}
	if !strings.Contains(err.Error(), "quay answer <session>") {
		t.Fatalf("the refusal says %q, want it to name how the command is called", err)
	}
}

// A flag this command does not take is refused by name rather than read as a session, which is what
// every removed flag is refused for: a word nobody meant became the value of an argument.
func TestAFlagAnswerDoesNotTakeIsRefused(t *testing.T) {
	client, session := aSessionThatAnswered(t, "the 14th")

	if _, err := runQuay(t, client, "answer", session.GetId(), "--everything"); err == nil {
		t.Fatal("a flag this command does not take was accepted")
	}
}

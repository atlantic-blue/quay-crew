package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// One word sends a task. It waits for the answer, and --dispatch lets go of it.
//
// Letting go used to be a word of its own, and before that it did not exist at all: the task was
// held in the client for as long as the work took, so the terminal was the weakest part of the crew.
// A task killed at seventeen minutes recorded "failed: model: run exited: signal: killed", said
// nothing about why, and the work was gone.
//
// The tests below hold the model open rather than timing it. What is being tested is whether a
// command comes back before its task does, and a test that waits a duration for that passes on a
// fast machine by accident.

// aHeldCrew is a crew whose model will not answer until the returned func lets it, and which says
// when a task has genuinely reached it.
func aHeldCrew(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *model.FakeRunner) {
	t.Helper()
	runner := &model.FakeRunner{
		Reply: "the electricity bill is due on the ninth",
		Gate:  make(chan struct{}), Started: make(chan struct{}),
	}
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client, runner
}

// The flag lets go: the command comes back while the model is still working.
//
// Run behind the test rather than in front of it, because a flag that stopped letting go would
// otherwise hold this test open for as long as the runner is held, which is forever, and a suite
// that hangs says nothing about what is wrong.
func TestTheDispatchFlagLetsGoOfTheTask(t *testing.T) {
	client, runner := aHeldCrew(t)

	said := letGoOf(t, client, "when is the electricity bill due")

	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}
	// The model has not answered and cannot have: nothing has let it go.
	if strings.Contains(said, "the electricity bill is due on the ninth") {
		t.Fatalf("--dispatch waited for the model after all: %q", said)
	}
	// And it says where the answer will be, because a blank where a reply used to be reads as a task
	// that answered nothing.
	for _, want := range []string{"quay task list", "quay attach", "handle "} {
		if !strings.Contains(said, want) {
			t.Fatalf("--dispatch does not say %q: %q", want, said)
		}
	}

	close(runner.Gate)
	handle := handleFrom(t, said)
	waitForTheAnswer(t, client, handle)
	history := mustRun(t, client, "task", "list", handle)
	if !strings.Contains(history, "the electricity bill is due on the ninth") {
		t.Fatalf("the answer never landed in the history:\n%s", history)
	}
}

// Somebody typing a short question is looking at the terminal, so the word on its own answers there.
func TestTheWordOnItsOwnWaitsForTheAnswer(t *testing.T) {
	client, runner := aHeldCrew(t)

	answered := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		if err := run(context.Background(), client,
			[]string{"task", "when is the electricity bill due"}, &out, ""); err != nil {
			answered <- "failed: " + err.Error()
			return
		}
		answered <- out.String()
	}()

	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}
	// Held open, so anything arriving here would be an answer nobody has given yet.
	select {
	case early := <-answered:
		t.Fatalf("quay task came back before the model did: %q", early)
	case <-time.After(50 * time.Millisecond):
	}

	close(runner.Gate)
	select {
	case said := <-answered:
		if !strings.Contains(said, "the electricity bill is due on the ninth") {
			t.Fatalf("quay task does not print the answer: %q", said)
		}
		if !strings.Contains(said, "handle ") {
			t.Fatalf("quay task does not say which session answered: %q", said)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("quay task never came back")
	}
}

// The work is the crew's now, so a caller that goes away does not take it. This is the failure that
// started it: seventeen minutes of work lost with the terminal that asked for it.
func TestATaskOutlivesTheCommandThatStartedIt(t *testing.T) {
	client, runner := aHeldCrew(t)

	handle := handleFrom(t, letGoOf(t, client, "read the repository"))
	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}

	// The caller is gone. Nothing here is holding the task open any more.
	close(runner.Gate)
	waitForTheAnswer(t, client, handle)

	session := onlySession(t, client)
	if session.GetStatus() != "idle" {
		t.Fatalf("the session reads %q after its task landed, want idle", session.GetStatus())
	}
	if history := mustRun(t, client, "task", "list", handle); strings.Contains(history, "failed") {
		t.Fatalf("the task failed once nobody was waiting for it:\n%s", history)
	}
}

// The three words this one replaced, each refused by name and each naming what to type.
//
// This is the way off, and it is the half that gets skipped: ask, dispatch and tasks are in fingers,
// in scripts and in notes, and every test written for a replacement passes while the old form does
// something quietly wrong. A silent alias would keep two spellings alive for one thing, and an
// unknown command reads as the tool being broken.
func TestTheThreeWordsOneWordReplacedAreRefusedByName(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	for _, testCase := range []struct {
		typed []string
		names string
	}{
		{[]string{"ask", "when is the electricity bill due"}, `quay task [<address>] "..."`},
		{[]string{"dispatch", "read the repository"}, `quay task --dispatch [<address>] "..."`},
		{[]string{"tasks", "3db6b81e"}, "quay task list <session>"},
		// With nothing after them too, because that is how a person checks what a word does.
		{[]string{"ask"}, `quay task [<address>] "..."`},
		{[]string{"dispatch"}, `quay task --dispatch [<address>] "..."`},
		{[]string{"tasks"}, "quay task list <session>"},
	} {
		err := refused(t, client, testCase.typed...)
		if !strings.Contains(err.Error(), testCase.names) {
			t.Errorf("quay %s is refused with %q, which does not name what to type instead",
				strings.Join(testCase.typed, " "), err)
		}
		if !strings.Contains(err.Error(), "there is no "+testCase.typed[0]+" command") {
			t.Errorf("quay %s does not say the word is gone: %q", strings.Join(testCase.typed, " "), err)
		}
	}

	// And nothing was started while they were being refused, because a refusal that half acts is
	// worse than one that does not act at all.
	if listed := mustRun(t, client, "sessions"); strings.Contains(listed, "idle") ||
		strings.Contains(listed, "running") {
		t.Errorf("a refused word started a session anyway:\n%s", listed)
	}
}

// A removed word must never be absorbed into anything else's argument, which is the failure this
// whole shape exists to avoid: `quay dispatch --project default "remember the number"` once made
// "--project default" the first two words of the message.
func TestARemovedWordNeverTakesTheMessageWithIt(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	for _, typed := range [][]string{
		{"ask", "remember the number"},
		{"dispatch", "remember the number"},
		{"tasks", "remember the number"},
	} {
		err := refused(t, client, typed...)
		if strings.Contains(err.Error(), "remember the number") {
			t.Errorf("quay %s took the message with it: %q", strings.Join(typed, " "), err)
		}
	}
}

// The flags somebody's fingers reach for when the word already says which of the two it is. Each one
// names the flag to type, and none of them is swallowed into the message.
func TestTheFlagsForWaitingAndLettingGoAreRefusedByName(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	for _, testCase := range []struct{ flag, names string }{
		{"--detach", "quay task --dispatch"},
		{"--wait", "quay task ["},
		{"--no-wait", "quay task --dispatch"},
	} {
		err := refused(t, client, "task", testCase.flag, "hello")
		if !strings.Contains(err.Error(), testCase.names) {
			t.Errorf("%s is refused with %q, which does not name what to type instead", testCase.flag, err)
		}
		// And the flag is never swallowed into the message, which is the defect this shape avoids.
		if strings.Contains(err.Error(), "hello") {
			t.Errorf("%s took the message with it: %q", testCase.flag, err)
		}
	}
}

// The word names itself when it is typed with nothing to say, and names both of its shapes, because
// somebody who got one of them wrong is as likely to have wanted the other.
func TestTheWordNamesBothOfItsShapesInItsUsage(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	for _, typed := range [][]string{{"task"}, {"task", flagDispatch}} {
		err := refused(t, client, typed...)
		for _, want := range []string{"usage: quay task [--dispatch] [<address>] <text>", "quay task list <session>"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("quay %s answers %q, which does not say %q", strings.Join(typed, " "), err, want)
			}
		}
	}
}

// The one that a rename creates on its own: `quay tasks <session>` becomes `quay task <session>`,
// which is a good command that sends the session's identifier to the model as a message. It succeeds,
// the operator gets no history, and nothing anywhere says the word changed.
func TestASessionOnItsOwnIsRefusedRatherThanSentAsAMessage(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "task", "hello")
	session := onlySession(t, client)

	for _, typed := range []string{session.GetId()[:8], session.GetHandle()[:8]} {
		err := refused(t, client, "task", typed)
		if !strings.Contains(err.Error(), "quay task list "+typed) {
			t.Errorf("quay task %s does not name the history command: %q", typed, err)
		}
		if !strings.Contains(err.Error(), "quay task <address> "+typed) {
			t.Errorf("quay task %s does not say how to send it as a message: %q", typed, err)
		}
	}

	// And nothing was sent: the session still holds the one task it was given above.
	history := mustRun(t, client, "task", "list", session.GetHandle()[:8])
	for _, typed := range []string{session.GetId()[:8], session.GetHandle()[:8]} {
		if strings.Contains(history, typed) {
			t.Errorf("the identifier was sent as a message after all:\n%s", history)
		}
	}
}

// An address with nothing after it is the same trap from the other side, and it says the same thing
// the usage does rather than sending the address as the message.
func TestAnAddressOnItsOwnIsRefusedRatherThanSentAsAMessage(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	err := refused(t, client, "task", "me/house-bills")
	if !strings.Contains(err.Error(), `quay task me/house-bills "..."`) {
		t.Errorf("quay task me/house-bills does not say what is missing: %q", err)
	}
	if listed := mustRun(t, client, "sessions"); strings.Contains(listed, "idle") {
		t.Errorf("an address on its own started a session anyway:\n%s", listed)
	}
}

// The flag is read in first position only. Anywhere else it is part of a sentence somebody meant to
// send, and a message with a word silently removed from it is the defect this shape exists to avoid.
func TestTheDispatchFlagIsOnlyReadInFirstPosition(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	err := refused(t, client, "task", "say", flagDispatch, "to the operator")
	if !strings.Contains(err.Error(), flagDispatch+" comes first") {
		t.Errorf("a flag in the middle of a message is refused with %q, which does not say why", err)
	}
}

// Reading a history back is not something letting go says anything about, so the two are refused
// together rather than one quietly winning.
func TestLettingGoAndReadingBackAreNotAskedForTogether(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	err := refused(t, client, "task", flagDispatch, "list", "3db6b81e")
	if !strings.Contains(err.Error(), "quay task list <session>") {
		t.Errorf("--dispatch with list is refused with %q, which does not name the command: %q", err, err)
	}
}

// letGoOf sends a task with the flag, and fails if the command has not come back.
//
// Run behind the test rather than in front of it, because a flag that stopped letting go would
// otherwise hold the test open for as long as the model is held, which is forever, and a suite that
// hangs says nothing about what is wrong.
func letGoOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, text string) string {
	t.Helper()
	cameBack := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		if err := run(context.Background(), client, []string{"task", flagDispatch, text}, &out, ""); err != nil {
			cameBack <- "failed: " + err.Error()
			return
		}
		cameBack <- out.String()
	}()
	select {
	case said := <-cameBack:
		if strings.HasPrefix(said, "failed: ") {
			t.Fatalf("quay task %s %q: %s", flagDispatch, text, said)
		}
		return said
	case <-time.After(5 * time.Second):
		t.Fatalf("quay task %s never came back, so it is waiting for a task nobody is waiting for",
			flagDispatch)
		return ""
	}
}

// handleFrom reads the handle out of what a command printed, the way the operator copies it.
func handleFrom(t *testing.T, said string) string {
	t.Helper()
	_, after, found := strings.Cut(said, "handle ")
	if !found {
		t.Fatalf("nothing printed a handle: %q", said)
	}
	return strings.TrimSuffix(strings.TrimSpace(after), ")")
}

// waitForTheAnswer waits until the crew has finished with a task it was let go of, so an assertion
// about what landed is never made against a task still in flight.
func waitForTheAnswer(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, handle string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		history, err := runQuay(t, client, "task", "list", handle)
		// A history with nothing in it yet, or with a task still in flight, is not a landed task.
		if err == nil && strings.TrimSpace(history) != "" &&
			!strings.Contains(history, "no tasks recorded") && !strings.Contains(history, "still running") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the task never landed")
}

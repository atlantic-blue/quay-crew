//go:build integration

package store_test

import (
	"context"
	"fmt"
	"io"
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

// Naming a conversation, over a real database, a real control plane and the real model adapter.
//
// Only the container is a double here, and it is the boundary the crew cannot cross in a test: it
// records the command line the model would have been run with and streams back what the runtime
// streams. Everything the defect was made of is real. The crew names the conversation, writes it to
// the database, hands it to the adapter, the adapter builds the command line, and attaching reads the
// name back out of the database while the task is still running.
//
// The unit tier proves each of those in isolation and none of them proves this: the whole failure was
// that the name arrived after the task, so a test that looks once the task has landed finds a crew
// that already knew the name and reports the defect as fixed while it is still there.

// heldSandbox is the container a task runs in: it records every command line, and holds the task open
// until a test lets it go, so the test can be an operator attaching while the model works.
type heldSandbox struct {
	mu   sync.Mutex
	ran  []sandbox.Spec
	hold chan struct{}
	// started is closed when the first command reaches it, so a test knows the task is genuinely under
	// way rather than inferring it from how long a call took.
	started chan struct{}
	once    sync.Once
}

func newHeldSandbox() *heldSandbox {
	return &heldSandbox{hold: make(chan struct{}), started: make(chan struct{})}
}

func (h *heldSandbox) Exec(ctx context.Context, spec sandbox.Spec) (sandbox.Process, error) {
	h.mu.Lock()
	h.ran = append(h.ran, spec)
	h.mu.Unlock()
	// Only a task is held, and only a task says one has started. Everything else a session's container
	// is asked to run happens before the task and answers straight away, as it does in a real one.
	if len(spec.Argv) == 0 || spec.Argv[0] != "claude" {
		return &streamed{body: strings.NewReader("")}, nil
	}
	h.once.Do(func() { close(h.started) })
	select {
	case <-h.hold:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// What the runtime streams back, carrying the conversation it was told to use. A runtime that
	// honours the flag reports the name it was given, and the crew reads this as its check.
	return &streamed{body: strings.NewReader(fmt.Sprintf(
		"{\"type\":\"result\",\"session_id\":%q,\"result\":\"done\"}\n", conversationOn(spec.Argv)))}, nil
}

func (h *heldSandbox) Close(context.Context) error { return nil }

// release lets every held task run to its answer.
func (h *heldSandbox) release() { close(h.hold) }

// waitForTask blocks until a task has reached the container.
func (h *heldSandbox) waitForTask(t *testing.T) {
	t.Helper()
	select {
	case <-h.started:
	case <-time.After(30 * time.Second):
		t.Fatal("no task reached the container")
	}
}

// commands is every command line the model was run with, read under its own lock because the test
// reads it while a task is in flight. A session's container is asked to run other things as well, the
// git identity and whatever a skill sets up among them, and none of those is a task.
func (h *heldSandbox) commands() [][]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([][]string, 0, len(h.ran))
	for _, spec := range h.ran {
		if len(spec.Argv) == 0 || spec.Argv[0] != "claude" {
			continue
		}
		out = append(out, append([]string(nil), spec.Argv...))
	}
	return out
}

type streamed struct{ body io.Reader }

func (s *streamed) Stdout() io.Reader { return s.body }
func (s *streamed) Wait() error       { return nil }
func (s *streamed) Stderr() string    { return "" }

// heldProvider hands the same held container to every session.
type heldProvider struct{ box *heldSandbox }

func (p *heldProvider) Create(context.Context, sandbox.Config) (sandbox.Sandbox, error) {
	return p.box, nil
}
func (p *heldProvider) Remove(context.Context, string) error           { return nil }
func (p *heldProvider) Stranded(context.Context) ([]string, error)     { return nil, nil }
func (p *heldProvider) Attached(context.Context, string) (bool, error) { return false, nil }

// conversationOn is the conversation a command line names, whichever of the two flags carries it.
func conversationOn(argv []string) string {
	for index, arg := range argv {
		if (arg == "--session-id" || arg == "--resume") && index+1 < len(argv) {
			return argv[index+1]
		}
	}
	return ""
}

// flagOn is how a command line names its conversation: started under, or resumed by.
func flagOn(argv []string) string {
	for _, arg := range argv {
		if arg == "--session-id" || arg == "--resume" {
			return arg
		}
	}
	return ""
}

// aCrewOnPostgresRunningTheModelAdapter is the real control plane, over the real database, running
// tasks through the real Claude Code adapter into a container a test can hold open.
func aCrewOnPostgresRunningTheModelAdapter(t *testing.T, box *heldSandbox) *controlplane.Server {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: model.NewClaudeCodeRunner(), Provider: &heldProvider{box: box},
		Secrets: secrets.NewMemory(),
	})
}

// The defect, in the shape it was found in: a session's first task is running, and the operator
// attaches to watch it.
//
// Everything asserted here is asserted while the task is still in flight, because that is the whole
// of it. The crew used to pass no name on a first task, read the name the runtime chose out of the
// output stream and record it once the task had landed, so for the life of that task the session held
// nothing, attaching found an empty field, named a second conversation and opened that one instead.
func TestAttachingToARunningFirstTaskOpensTheConversationTheTaskIsInOnPostgres(t *testing.T) {
	box := newHeldSandbox()
	s := aCrewOnPostgresRunningTheModelAdapter(t, box)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "take your time", Detach: true})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	box.waitForTask(t)

	// The session knows which conversation it is working in, now, rather than when the task is over.
	held, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	named := held.GetSession().GetModelSessionId()
	if named == "" {
		t.Fatal("the session is running a task and holds no conversation, so nothing can open the one it is in")
	}
	if got := held.GetSession().GetStatus(); got != controlplane.StatusRunning {
		t.Fatalf("the session reads %q, and this case is about attaching while a task runs", got)
	}

	// The task is running in that conversation, under the flag that starts one, because nothing has
	// written a transcript for it yet.
	commands := box.commands()
	if len(commands) != 1 {
		t.Fatalf("the container was asked to run %d commands, want the one task", len(commands))
	}
	if got := conversationOn(commands[0]); got != named {
		t.Fatalf("the task is running in conversation %q and the session holds %q", got, named)
	}
	if got := flagOn(commands[0]); got != "--session-id" {
		t.Fatalf("the first task names its conversation with %q, want --session-id: there is no "+
			"transcript to resume, and resuming one exits saying no conversation was found", got)
	}

	// The operator attaches, and lands in the conversation doing the work rather than beside it.
	spec, err := s.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("AttachSession while the task runs: %v", err)
	}
	opened := conversationIn(spec.GetArgv())
	if opened != named {
		t.Fatalf("attaching opens conversation %q and the session holds %q", opened, named)
	}
	if opened != conversationOn(box.commands()[0]) {
		t.Fatalf("attaching opens conversation %q and the task is running in %q, so the operator "+
			"is watching an empty conversation beside the work", opened, conversationOn(box.commands()[0]))
	}
	// Attaching must not have named anything: a second name here is the defect wearing a fix.
	after, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got := after.GetSession().GetModelSessionId(); got != named {
		t.Fatalf("the session held conversation %q before the attach and %q after it", named, got)
	}

	box.release()

	// And once it lands, the name is still the same one: what the runtime reported is a check now, not
	// the source. A second task continues that conversation rather than starting it again.
	waitForConversation(t, s, sent.GetId(), named)
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Handle: sent.GetHandle(), Text: "and again",
	}); err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	commands = box.commands()
	if len(commands) != 2 {
		t.Fatalf("the container ran %d commands, want the two tasks", len(commands))
	}
	if got := conversationOn(commands[1]); got != named {
		t.Fatalf("the second task runs in conversation %q, want the same %q", got, named)
	}
	if got := flagOn(commands[1]); got != "--resume" {
		t.Fatalf("the second task names its conversation with %q, want --resume: starting a "+
			"conversation the runtime already has is refused as a name already in use", got)
	}
}

// A session carried over from a crew that named conversations after the task, caught mid task. The
// crew cannot know that conversation's name until the task lands, so attaching says so rather than
// naming a second one and opening an empty conversation beside the work.
func TestAttachingToARunningSessionThatNamedItsOwnConversationIsRefusedOnPostgres(t *testing.T) {
	box := newHeldSandbox()
	s := aCrewOnPostgresRunningTheModelAdapter(t, box)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	kept, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	session, _, err := kept.FindOrCreateSession(ctx, project, "carried-over", store.Birth{})
	if err != nil {
		t.Fatalf("FindOrCreateSession: %v", err)
	}
	// Running, with no conversation on it: exactly what the old crew left behind while a first task
	// was in flight.
	if err := kept.RecordTask(ctx, session.GetId(), "", controlplane.StatusRunning); err != nil {
		t.Fatalf("RecordTask: %v", err)
	}

	_, err = s.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: session.GetId()})
	if err == nil {
		t.Fatal("attaching was allowed, and it would have opened a second conversation beside the task")
	}
	for _, want := range []string{"named its own conversation", "second conversation"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal is %q, want it to say %q so the operator knows to wait", err.Error(), want)
		}
	}
	// Nothing was named, so nothing was orphaned: the task's own conversation lands on the session
	// when it finishes.
	after, err := kept.GetSession(ctx, session.GetId())
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got := after.GetModelSessionId(); got != "" {
		t.Fatalf("the refusal named conversation %q anyway", got)
	}
}

// waitForConversation waits for a detached task to land and leave the session holding the name the
// crew gave it.
func waitForConversation(t *testing.T, s *controlplane.Server, session, want string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got, err := s.GetSession(context.Background(), &quaycrewv1.GetSessionRequest{Id: session})
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.GetSession().GetStatus() == controlplane.StatusIdle {
			if held := got.GetSession().GetModelSessionId(); held != want {
				t.Fatalf("the task landed and the session holds conversation %q, want the %q it ran in", held, want)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the task never landed")
}

// conversationIn is the conversation an attach specification opens, which is the argument after the
// command that opens one.
func conversationIn(argv []string) string {
	for index, arg := range argv {
		if arg == sandbox.OpenConversation && index+1 < len(argv) {
			return argv[index+1]
		}
	}
	return ""
}

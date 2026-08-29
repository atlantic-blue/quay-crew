package controlplane_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// Naming a conversation, driven through the real control plane and the real model adapter.
//
// The container is the only double, and it is the boundary a test cannot cross: it records the
// command line the model would have been run with, holds the task open so the test can be an operator
// attaching while the model works, and streams back what a runtime streams. The command line is the
// point. Everything else about this could be right while the flag that names the conversation is
// missing, and a task whose command line names no conversation is the whole defect.

// heldSandbox is a container that records what it was asked to run and holds it open until let go.
type heldSandbox struct {
	mu      sync.Mutex
	ran     []sandbox.Spec
	hold    chan struct{}
	started chan struct{}
	once    sync.Once
	// ignores makes it a runtime that names its own conversation whatever it was told, which is the
	// one thing the identifier in the output stream is still there to catch.
	ignores string
}

func newHeldSandbox() *heldSandbox {
	return &heldSandbox{hold: make(chan struct{}), started: make(chan struct{})}
}

func (h *heldSandbox) Exec(ctx context.Context, spec sandbox.Spec) (sandbox.Process, error) {
	h.mu.Lock()
	h.ran = append(h.ran, spec)
	ignores := h.ignores
	h.mu.Unlock()
	// Only a task is held, and only a task says one has started. Everything else a session's container
	// is asked to run happens before the task and answers straight away, the way it does in a real one.
	if len(spec.Argv) == 0 || spec.Argv[0] != "claude" {
		return &streamedResult{body: strings.NewReader("")}, nil
	}
	h.once.Do(func() { close(h.started) })
	select {
	case <-h.hold:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	reported := conversationOn(spec.Argv)
	if ignores != "" {
		reported = ignores
	}
	return &streamedResult{body: strings.NewReader(fmt.Sprintf(
		"{\"type\":\"result\",\"session_id\":%q,\"result\":\"done\"}\n", reported))}, nil
}

func (h *heldSandbox) Close(context.Context) error { return nil }

func (h *heldSandbox) release() { close(h.hold) }

func (h *heldSandbox) waitForTask(t *testing.T) {
	t.Helper()
	select {
	case <-h.started:
	case <-time.After(30 * time.Second):
		t.Fatal("no task reached the container")
	}
}

// commands is every command line the model was run with, read under its own lock because a test reads
// it while a task is in flight. A session's container is asked to run other things as well, the git
// identity and whatever a skill sets up among them, and none of those is a task.
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

type streamedResult struct{ body io.Reader }

func (s *streamedResult) Stdout() io.Reader { return s.body }
func (s *streamedResult) Wait() error       { return nil }
func (s *streamedResult) Stderr() string    { return "" }

// heldProvider hands the same held container to every session.
type heldProvider struct{ box *heldSandbox }

func (p *heldProvider) Create(context.Context, sandbox.Config) (sandbox.Sandbox, error) {
	return p.box, nil
}
func (p *heldProvider) Remove(context.Context, string) error                 { return nil }
func (p *heldProvider) Stranded(context.Context) ([]string, error)           { return nil, nil }
func (p *heldProvider) Attached(context.Context, string) (bool, error)       { return false, nil }
func (p *heldProvider) RuntimeRunning(context.Context, string) (bool, error) { return false, nil }

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

// conversationOpenedBy is the conversation an attach specification opens, which is the argument after
// the command that opens one.
func conversationOpenedBy(argv []string) string {
	for index, arg := range argv {
		if arg == sandbox.OpenConversation && index+1 < len(argv) {
			return argv[index+1]
		}
	}
	return ""
}

// aCrewRunningTheModelAdapter is the control plane running tasks through the real Claude Code adapter
// into a container the test holds open.
func aCrewRunningTheModelAdapter(box *heldSandbox) *controlplane.Server {
	return controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: model.NewClaudeCodeRunner(), Provider: &heldProvider{box: box},
		Secrets: secrets.NewMemory(),
	})
}

// The defect, in the shape it was found in: a session's first task is running, and the operator
// attaches to watch it.
//
// Everything here is asserted while the task is in flight, because that is the whole of it. The crew
// used to pass no name on a first task, read the name the runtime chose out of the output stream, and
// record it once the task had landed. So for the life of that task the session held nothing, attaching
// found an empty field, named a second conversation and opened that one instead. A test that looks
// once the task has landed finds a crew that already knew the name, and reports this as fixed while
// it is still there.
func TestAttachingToARunningFirstTaskOpensTheConversationTheTaskIsIn(t *testing.T) {
	box := newHeldSandbox()
	s := aCrewRunningTheModelAdapter(box)
	ctx := context.Background()
	_, project := newProject(t, s)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "take your time", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	box.waitForTask(t)

	named := conversationHeldBy(t, s, sent.GetId())
	if named == "" {
		t.Fatal("the session is running a task and holds no conversation, so nothing can open the one it is in")
	}

	// The task is running in that conversation, under the flag that starts one: nothing has written a
	// transcript for it yet, and resuming a name with nothing behind it exits saying so.
	commands := box.commands()
	if len(commands) != 1 {
		t.Fatalf("the container was asked to run %d commands, want the one task", len(commands))
	}
	if got := conversationOn(commands[0]); got != named {
		t.Fatalf("the task is running in conversation %q and the session holds %q", got, named)
	}
	if got := flagOn(commands[0]); got != "--session-id" {
		t.Fatalf("the first task names its conversation with %q, want --session-id", got)
	}

	// The operator attaches, and lands in the conversation doing the work rather than beside it.
	spec, err := s.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("AttachSession while the task runs: %v", err)
	}
	opened := conversationOpenedBy(spec.GetArgv())
	if opened != conversationOn(box.commands()[0]) {
		t.Fatalf("attaching opens conversation %q and the task is running in %q, so the operator is "+
			"watching an empty conversation beside the job", opened, conversationOn(box.commands()[0]))
	}
	// Attaching named nothing of its own. A second name here is the defect wearing a fix.
	if got := conversationHeldBy(t, s, sent.GetId()); got != named {
		t.Fatalf("the session held conversation %q before the attach and %q after it", named, got)
	}

	box.release()
	waitForIdle(t, s, sent.GetId())

	// The name survives the task landing: what the runtime reports is a check now, not the source.
	if got := conversationHeldBy(t, s, sent.GetId()); got != named {
		t.Fatalf("the task landed and the session holds conversation %q, want the %q it ran in", got, named)
	}
}

// The second task continues the conversation the first one started, and says so with the other flag.
// Getting this pair the wrong way round fails the task either way: resuming a name nothing has written
// exits saying no conversation was found, and starting a name the runtime already has is refused as
// one already in use.
func TestASecondTaskResumesTheConversationTheFirstOneStarted(t *testing.T) {
	box := newHeldSandbox()
	box.release()
	s := aCrewRunningTheModelAdapter(box)
	ctx := context.Background()
	_, project := newProject(t, s)

	first, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Handle: first.GetHandle(), Text: "and again",
	}); err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}

	commands := box.commands()
	if len(commands) != 2 {
		t.Fatalf("the container ran %d commands, want the two tasks", len(commands))
	}
	named := conversationHeldBy(t, s, first.GetId())
	for index, want := range []string{"--session-id", "--resume"} {
		if got := flagOn(commands[index]); got != want {
			t.Errorf("task %d names its conversation with %q, want %q", index+1, got, want)
		}
		if got := conversationOn(commands[index]); got != named {
			t.Errorf("task %d runs in conversation %q, want the session's %q", index+1, got, named)
		}
	}
}

// A session nobody has dispatched to has no conversation, and opening it is what names one. Attaching
// is still the moment a conversation starts to exist for a session whose first task failed before the
// model ran, and that session used to sit in the listing with no way to open it at all.
func TestAttachingToASessionThatHasNeverRunATaskNamesItsConversation(t *testing.T) {
	kept := store.NewMemory()
	box := newHeldSandbox()
	box.release()
	s := controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: model.NewClaudeCodeRunner(), Provider: &heldProvider{box: box},
		Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()
	_, project := newProject(t, s)

	session := aSessionNobodyHasDispatchedTo(t, ctx, kept, project)

	spec, err := s.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: session})
	if err != nil {
		t.Fatalf("AttachSession: %v", err)
	}
	named := conversationHeldBy(t, s, session)
	if named == "" {
		t.Fatal("the session still holds no conversation, so nothing can be attributed to what is typed in it")
	}
	if got := conversationOpenedBy(spec.GetArgv()); got != named {
		t.Fatalf("the command opens conversation %q and the session holds %q", got, named)
	}

	// Opening it again is the same conversation, or the history from the first open is orphaned.
	again, err := s.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: session})
	if err != nil {
		t.Fatalf("AttachSession twice: %v", err)
	}
	if got := conversationOpenedBy(again.GetArgv()); got != named {
		t.Fatalf("opening it again opens conversation %q, want the %q it was given", got, named)
	}
}

// A stopped session opens, and opens the conversation it already holds. Restarting it is part of
// opening it, because a drain puts every live session down and every one of them refused to open
// afterwards.
func TestAStoppedSessionOpensTheConversationItAlreadyHolds(t *testing.T) {
	box := newHeldSandbox()
	box.release()
	s := aCrewRunningTheModelAdapter(box)
	ctx := context.Background()
	_, project := newProject(t, s)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	named := conversationHeldBy(t, s, sent.GetId())
	if _, err := s.StopSession(ctx, &quaycrewv1.StopSessionRequest{Id: sent.GetId()}); err != nil {
		t.Fatalf("StopSession: %v", err)
	}

	spec, err := s.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("AttachSession on a stopped session: %v", err)
	}
	if got := conversationOpenedBy(spec.GetArgv()); got != named {
		t.Fatalf("it opens conversation %q, want the %q the session was working in", got, named)
	}
}

// A session carried over from a crew that named conversations after the task, caught mid task. Its
// conversation has a name and the crew cannot know it until the task lands, so attaching says so
// rather than naming a second one and opening an empty conversation beside the job.
func TestAttachingToARunningSessionThatNamedItsOwnConversationIsRefused(t *testing.T) {
	kept := store.NewMemory()
	box := newHeldSandbox()
	s := controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: model.NewClaudeCodeRunner(), Provider: &heldProvider{box: box},
		Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()
	_, project := newProject(t, s)

	session, _, err := kept.FindOrCreateSession(ctx, project, "carried-over", store.Birth{})
	if err != nil {
		t.Fatalf("FindOrCreateSession: %v", err)
	}
	// Running, with no conversation on it: what the old crew left behind while a first task was in
	// flight, and the one state where naming a conversation here is the defect rather than the fix.
	if err := kept.RecordTask(ctx, session.GetId(), "", controlplane.StatusRunning); err != nil {
		t.Fatalf("RecordTask: %v", err)
	}

	if _, err := s.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: session.GetId()}); err == nil {
		t.Fatal("attaching was allowed, and it would have opened a second conversation beside the task")
	} else {
		for _, want := range []string{"named its own conversation", "second conversation"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal is %q, want it to say %q so the operator knows to wait", err.Error(), want)
			}
		}
	}
	// Nothing was named, so nothing was orphaned: the conversation the task is in lands on the session
	// when it finishes.
	after, err := kept.GetSession(ctx, session.GetId())
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got := after.GetModelSessionId(); got != "" {
		t.Fatalf("the refusal named conversation %q anyway", got)
	}
}

// A runtime that ignores the name it was given keeps the crew's own name, because that is the name
// every later task and every attach will carry. What the crew must not do is quietly adopt the other
// one and leave the two disagreeing about which conversation this session is.
func TestARuntimeThatIgnoresTheNameLeavesTheCrewHoldingItsOwn(t *testing.T) {
	box := newHeldSandbox()
	box.ignores = "1f2e3d4c-5b6a-4978-8695-a4b3c2d1e0f9"
	box.release()
	s := aCrewRunningTheModelAdapter(box)
	ctx := context.Background()
	_, project := newProject(t, s)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	held := conversationHeldBy(t, s, sent.GetId())
	if held == box.ignores {
		t.Fatal("the crew adopted the name the runtime chose, so its own name now points at nothing")
	}
	if got := conversationOn(box.commands()[0]); got != held {
		t.Fatalf("the task asked for conversation %q and the session holds %q", got, held)
	}
}

// conversationHeldBy is the conversation a session holds, read back over the interface a client uses.
func conversationHeldBy(t *testing.T, s *controlplane.Server, session string) string {
	t.Helper()
	got, err := s.GetSession(context.Background(), &quaycrewv1.GetSessionRequest{Id: session})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return got.GetSession().GetModelSessionId()
}

// aSessionNobodyHasDispatchedTo is a session that exists and has never run a task, which is what a
// first task that failed before the model ran leaves behind: a session in the listing holding no
// conversation at all.
func aSessionNobodyHasDispatchedTo(t *testing.T, ctx context.Context, kept store.Store, project string) string {
	t.Helper()
	session, _, err := kept.FindOrCreateSession(ctx, project, "never-dispatched", store.Birth{})
	if err != nil {
		t.Fatalf("FindOrCreateSession: %v", err)
	}
	if session.GetModelSessionId() != "" {
		t.Fatalf("a session nobody has dispatched to already holds conversation %q",
			session.GetModelSessionId())
	}
	return session.GetId()
}

// waitForIdle waits for a detached task to land.
func waitForIdle(t *testing.T, s *controlplane.Server, session string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got, err := s.GetSession(context.Background(), &quaycrewv1.GetSessionRequest{Id: session})
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.GetSession().GetStatus() == controlplane.StatusIdle {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the task never landed")
}

// A conversation the operator opened by hand, before any task ran in it. The transcript on the host
// is what says it has been opened, so the first task resumes it rather than starting it again, and
// this is the case a crew's own memory cannot answer: the transcript was written by somebody typing
// in a container, and this control plane never saw it happen.
func TestATaskResumesAConversationTheOperatorOpenedByHand(t *testing.T) {
	dir := t.TempDir()
	kept := store.NewMemory()
	box := newHeldSandbox()
	box.release()
	s := controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: model.NewClaudeCodeRunner(), Provider: &heldProvider{box: box},
		Secrets: secrets.NewMemory(), Storage: sandbox.Storage{Dir: dir, Host: dir},
	})
	ctx := context.Background()
	workspace, project := newProject(t, s)

	session := aSessionNobodyHasDispatchedTo(t, ctx, kept, project)
	if _, err := s.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: session}); err != nil {
		t.Fatalf("AttachSession: %v", err)
	}
	named := conversationHeldBy(t, s, session)

	// What the operator typing in that container leaves behind: a transcript under the name the crew
	// gave, in the workspace's conversation store.
	transcript := filepath.Join(dir, "workspaces", workspace, "claude", "projects", "-home-agent-workspace")
	if err := os.MkdirAll(transcript, 0o777); err != nil {
		t.Fatalf("making the conversation store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(transcript, named+sandbox.ConversationFile),
		[]byte("{}\n"), 0o666); err != nil {
		t.Fatalf("writing the transcript: %v", err)
	}

	sessions, err := s.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions.GetSessions()) != 1 {
		t.Fatalf("the project holds %d sessions, want the one", len(sessions.GetSessions()))
	}
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Handle: sessions.GetSessions()[0].GetHandle(), Text: "carry on",
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	commands := box.commands()
	if len(commands) != 1 {
		t.Fatalf("the container ran %d tasks, want the one", len(commands))
	}
	if got := flagOn(commands[0]); got != "--resume" {
		t.Fatalf("the task names its conversation with %q, want --resume: the operator has already "+
			"typed in it, and starting a conversation the runtime holds is refused as a name in use", got)
	}
	if got := conversationOn(commands[0]); got != named {
		t.Fatalf("the task runs in conversation %q, want the %q the operator opened", got, named)
	}
}

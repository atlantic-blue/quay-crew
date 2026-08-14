package controlplane

import (
	"context"
	"strings"
	"sync"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// describingRunner answers a turn and a description differently, because a fake that answers both
// with the same string cannot tell whether the crew described the thread or just stored the turn.
type describingRunner struct {
	mu sync.Mutex
	// Described counts the descriptions asked for, which is what proves a crew with it switched off
	// asks for none rather than asking and throwing the answer away.
	Described int
	// DescribeErr fails the describing call alone, leaving ordinary turns working.
	DescribeErr error
	// Says is what a description comes back as, before the crew tidies it.
	Says string
	// Echoes answers with the question, the way the echo backend continuous integration runs does.
	Echoes bool
}

func (r *describingRunner) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	// A description is the call that carries no conversation to resume and asks the question this
	// crew asks. Matching on the question rather than on the absence alone, so an ordinary first turn
	// is never counted as one.
	if strings.Contains(req.Text, "say what this conversation is for") {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.Described++
		if r.DescribeErr != nil {
			return model.Response{}, r.DescribeErr
		}
		if r.Echoes {
			return model.Response{Reply: req.Text}, nil
		}
		says := r.Says
		if says == "" {
			says = "blog post about the agentic harness"
		}
		return model.Response{Reply: says}, nil
	}
	return model.Response{Reply: "ok", ModelSessionID: "conversation-1"}, nil
}

// describingCrew is a crew with one thread, and the parts a case needs to look at afterwards.
type describingCrew struct {
	server   *Server
	store    store.Store
	runner   *describingRunner
	threadID string
	// handle is what a dispatch continues a conversation by. Dispatching with the session id instead
	// silently starts a new thread whose handle happens to be that id, so every turn after the first
	// lands somewhere else and no thread ever reaches a second turn.
	handle  string
	project string
}

func describingCrewOf(t *testing.T, every int) *describingCrew {
	t.Helper()
	runner := &describingRunner{}
	memory := store.NewMemory()
	server := NewServer(Config{
		Store: memory, Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		DescribeEvery: every,
	})
	ctx := context.Background()
	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "me"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return &describingCrew{server: server, store: memory, runner: runner, project: project.GetProject().GetId()}
}

// dispatch runs a turn and waits for any describing behind it, so a case reads the result rather than
// racing it. Describing runs behind the answer on purpose, and a test that slept would be slow when
// it passed and flaky when it failed.
func (c *describingCrew) dispatch(t *testing.T, text string) string {
	t.Helper()
	resp, err := c.server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: c.project, Handle: c.handle, Text: text,
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if c.threadID == "" {
		c.threadID, c.handle = resp.GetId(), resp.GetHandle()
	}
	if resp.GetId() != c.threadID {
		t.Fatalf("a turn landed on thread %s, not %s: the conversation is not being continued",
			resp.GetId(), c.threadID)
	}
	c.server.describing.Wait()
	return resp.GetReply()
}

func (c *describingCrew) description(t *testing.T) string {
	t.Helper()
	thread, err := c.store.GetSession(context.Background(), c.threadID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return thread.GetDescription()
}

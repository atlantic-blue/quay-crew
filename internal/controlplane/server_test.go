package controlplane_test

import (
	"context"
	"net"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func newServer(runner model.Runner) *controlplane.Server {
	return controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
}

func TestCreateAndListWorkspaces(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	ctx := context.Background()

	created, err := s.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if created.GetWorkspace().GetId() == "" || created.GetWorkspace().GetName() != "acme" {
		t.Fatalf("bad workspace: %+v", created.GetWorkspace())
	}

	list, err := s.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(list.GetWorkspaces()) != 1 {
		t.Fatalf("want 1 workspace, got %d", len(list.GetWorkspaces()))
	}
}

func TestGetWorkspaceNotFound(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, err := s.GetWorkspace(context.Background(), &quaycrewv1.GetWorkspaceRequest{Id: "nope"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestDispatchStartsAndContinuesSession(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	s := newServer(runner)
	ctx := context.Background()

	wid, pid := newProject(t, s)
	_ = wid

	first, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if first.GetReply() != "done" || first.GetHandle() == "" {
		t.Fatalf("bad dispatch response: %+v", first)
	}
	// The system names the conversation before the exec starts, and the first exec starts it rather than
	// resuming it. Both halves matter: a first exec that carried no name left the runtime to name its
	// own conversation and tell nobody until the exec was over.
	named := runner.LastReq.ModelSessionID
	if named == "" {
		t.Fatal("the first exec carries no conversation, so the runtime names one the system cannot see")
	}
	if runner.LastReq.ConversationStarted {
		t.Fatal("the first exec resumes a conversation nothing has written, which exits saying there is none")
	}
	held, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: first.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got := held.GetSession().GetModelSessionId(); got != named {
		t.Fatalf("the session holds conversation %q and its exec ran in %q", got, named)
	}

	// Continue the same session: the runner should be asked to resume the model session.
	second, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Handle: first.GetHandle(), Text: "more"})
	if err != nil {
		t.Fatalf("Dispatch continue: %v", err)
	}
	if second.GetId() != first.GetId() {
		t.Fatalf("continue should reuse session: %q vs %q", second.GetId(), first.GetId())
	}
	if runner.LastReq.ModelSessionID != named {
		t.Fatalf("the second exec runs in conversation %q, want the first exec's %q",
			runner.LastReq.ModelSessionID, named)
	}
	if !runner.LastReq.ConversationStarted {
		t.Fatal("the second exec starts the conversation again, which is refused as a name already in use")
	}
}

func TestDispatchUnknownWorkspace(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, err := s.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{Project: "ghost", Text: "hi"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestDispatchInjectsTheWorkspaceSubscriptionToken(t *testing.T) {
	runner := &model.FakeRunner{Reply: "ok"}
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()

	wid, pid := newProject(t, s)

	if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{Workspace: wid, Key: model.ClaudeCodeOAuthTokenEnv, Value: "tok-xyz"}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "hello"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if got := runner.LastReq.Env[model.ClaudeCodeOAuthTokenEnv]; got != "tok-xyz" {
		t.Fatalf("exec env[%s] = %q, want tok-xyz", model.ClaudeCodeOAuthTokenEnv, got)
	}
}

// A session is told which session it is whatever the workspace holds, because it names its own
// working tree in the shared volume after that. Everything else here comes from a secret, so this is
// what an exec with none carries.
func TestDispatchWithoutASecretRunsWithNoExtraEnv(t *testing.T) {
	runner := &model.FakeRunner{Reply: "ok"}
	s := newServer(runner)
	ctx := context.Background()

	_, pid := newProject(t, s)
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "hello"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(runner.LastReq.Env) != 1 || runner.LastReq.Env[sandbox.SessionIDEnv] == "" {
		t.Fatalf("exec env = %v, want only %s when no secret is set", runner.LastReq.Env, sandbox.SessionIDEnv)
	}
}

func TestSetSecretStoresValue(t *testing.T) {
	secretStore := secrets.NewMemory()
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{}, Provider: &sandbox.FakeProvider{}, Secrets: secretStore,
	})
	ctx := context.Background()

	wid, _ := newProject(t, s)

	if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{Workspace: wid, Key: "token", Value: "s3cret"}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	got, err := secretStore.Get(ctx, wid, "token")
	if err != nil || got != "s3cret" {
		t.Fatalf("secret not stored: got %q err %v", got, err)
	}
}

func TestSessionSandboxLifecycle(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"}, Provider: provider, Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()

	wid, pid := newProject(t, s)
	_ = wid

	// Two execs on the same session must share one sandbox (created once, not per exec).
	first, _ := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "one"})
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Handle: first.GetHandle(), Text: "two"}); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	if len(provider.Created) != 1 {
		t.Fatalf("expected 1 sandbox for the session, got %d (%v)", len(provider.Created), provider.Created)
	}
	if provider.Created[0].ID != first.GetId() {
		t.Fatalf("sandbox created for %q, want session %q", provider.Created[0].ID, first.GetId())
	}

	// Stopping the session tears its sandbox down.
	if _, err := s.StopSession(ctx, &quaycrewv1.StopSessionRequest{Id: first.GetId()}); err != nil {
		t.Fatalf("StopSession: %v", err)
	}
	if !provider.Boxes[0].Closed {
		t.Fatal("stopping the session did not close its sandbox")
	}
}

// TestOverGrpc exercises the full gRPC path over an in memory listener.
func TestOverGrpc(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, newServer(&model.FakeRunner{Reply: "ok"}))
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := quaycrewv1.NewControlPlaneServiceClient(conn)

	ctx := context.Background()
	workspace, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace over grpc: %v", err)
	}
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject over grpc: %v", err)
	}
	dispatch, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project.GetProject().GetId(), Text: "hi"})
	if err != nil {
		t.Fatalf("Dispatch over grpc: %v", err)
	}
	if dispatch.GetReply() != "ok" {
		t.Fatalf("reply = %q, want ok", dispatch.GetReply())
	}
}

// newProject creates a workspace and a project inside it: the smallest setup a dispatch needs now
// that a session lives inside a project.
func newProject(t *testing.T, s *controlplane.Server) (workspaceID, projectID string) {
	t.Helper()
	ctx := context.Background()
	workspace, err := s.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return workspace.GetWorkspace().GetId(), project.GetProject().GetId()
}

// hangsUpRunner cancels the caller's context while the exec runs, which is what a client
// disconnecting after a long exec does to the dispatch path.
type hangsUpRunner struct {
	model.FakeRunner
	hangUp context.CancelFunc
}

func (r *hangsUpRunner) Run(ctx context.Context, box sandbox.Sandbox, req model.Request) (model.Response, error) {
	r.hangUp()
	return r.FakeRunner.Run(ctx, box, req)
}

// contextHonouringStore refuses writes on a dead context, the way Postgres does and the memory
// store does not. Without it this test would pass whatever the dispatch path did.
type contextHonouringStore struct {
	store.Store
}

func (s contextHonouringStore) AppendExec(ctx context.Context, exec *quaycrewv1.Exec, workspace, project, session string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Store.AppendExec(ctx, exec, workspace, project, session)
}

// A caller hanging up after a long exec must not lose the record of the very exec they were waiting
// on: history is written on a context detached from the request's.
func TestHistorySurvivesTheCallerHangingUp(t *testing.T) {
	ctx, hangUp := context.WithCancel(context.Background())
	defer hangUp()

	runner := &hangsUpRunner{FakeRunner: model.FakeRunner{Reply: "done"}, hangUp: hangUp}
	s := controlplane.NewServer(controlplane.Config{
		Store:    contextHonouringStore{Store: store.NewMemory()},
		Runner:   runner,
		Provider: &sandbox.FakeProvider{},
		Secrets:  secrets.NewMemory(),
	})

	workspace, err := s.CreateWorkspace(context.Background(), &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := s.CreateProject(context.Background(), &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	dispatched, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	listed, err := s.ListExecs(context.Background(), &quaycrewv1.ListExecsRequest{Session: dispatched.GetId()})
	if err != nil {
		t.Fatalf("ListExecs: %v", err)
	}
	if len(listed.GetExecs()) != 1 {
		t.Fatalf("the session has %d execs after the caller hung up, want the 1 that ran", len(listed.GetExecs()))
	}
	if listed.GetExecs()[0].GetReply() != "done" {
		t.Fatalf("the recorded exec says %q, want the reply that ran", listed.GetExecs()[0].GetReply())
	}
}

// The token a second time, under a name the Claude Code command line leaves alone.
//
// The CLI removes CLAUDE_CODE_OAUTH_TOKEN from the environment of every process it starts, by that
// name and no other, so the prompt hook fired on a message held no credential and every message went
// unanalysed. Nothing said so: the hook fails open, and the only record was the word "no answer" in a
// file in /tmp. The value is the same value, and only the name is new.
func TestDispatchCarriesTheSubscriptionTokenUnderTheNameThatSurvivesIntoAHook(t *testing.T) {
	runner := &model.FakeRunner{Reply: "ok"}
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()

	wid, pid := newProject(t, s)

	if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Workspace: wid, Key: model.ClaudeCodeOAuthTokenEnv, Value: "tok-xyz"}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "hello"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if got := runner.LastReq.Env[model.ModelTokenEnv]; got != "tok-xyz" {
		t.Fatalf("exec env[%s] = %q, want tok-xyz, and without it a hook cannot ask a model anything",
			model.ModelTokenEnv, got)
	}
	if got := runner.LastReq.Env[model.ClaudeCodeOAuthTokenEnv]; got != "tok-xyz" {
		t.Fatalf("exec env[%s] = %q, want tok-xyz: the second name is as well as, never instead of",
			model.ClaudeCodeOAuthTokenEnv, got)
	}
}

// A workspace with no subscription token carries neither name. An empty credential reads as
// configured, and the hook would report being logged out with a variable set.
func TestAWorkspaceWithNoSubscriptionTokenCarriesNeitherName(t *testing.T) {
	runner := &model.FakeRunner{Reply: "ok"}
	s := newServer(runner)
	ctx := context.Background()

	_, pid := newProject(t, s)
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "hello"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if got, set := runner.LastReq.Env[model.ModelTokenEnv]; set {
		t.Fatalf("exec env carries %s=%q, and nobody set a token", model.ModelTokenEnv, got)
	}
}

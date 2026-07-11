package controlplane_test

import (
	"context"
	"net"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func newServer(runner model.Runner) *controlplane.Server {
	return controlplane.NewServer(runner, &sandbox.FakeProvider{}, secrets.NewMemory())
}

func TestCreateAndListProjects(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	ctx := context.Background()

	created, err := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if created.GetProject().GetId() == "" || created.GetProject().GetName() != "acme" {
		t.Fatalf("bad project: %+v", created.GetProject())
	}

	list, err := s.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list.GetProjects()) != 1 {
		t.Fatalf("want 1 project, got %d", len(list.GetProjects()))
	}
}

func TestGetProjectNotFound(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, err := s.GetProject(context.Background(), &quaycrewv1.GetProjectRequest{Id: "nope"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestDispatchStartsAndContinuesThread(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done", SessionID: "model-1"}
	s := newServer(runner)
	ctx := context.Background()

	project, _ := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
	pid := project.GetProject().GetId()

	first, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if first.GetReply() != "done" || first.GetThreadId() == "" {
		t.Fatalf("bad dispatch response: %+v", first)
	}
	if runner.LastReq.ModelSessionID != "" {
		t.Fatalf("first turn should not resume, got %q", runner.LastReq.ModelSessionID)
	}

	// Continue the same thread: the runner should be asked to resume the model session.
	second, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, ThreadId: first.GetThreadId(), Text: "more"})
	if err != nil {
		t.Fatalf("Dispatch continue: %v", err)
	}
	if second.GetSessionId() != first.GetSessionId() {
		t.Fatalf("continue should reuse session: %q vs %q", second.GetSessionId(), first.GetSessionId())
	}
	if runner.LastReq.ModelSessionID != "model-1" {
		t.Fatalf("continue should resume model-1, got %q", runner.LastReq.ModelSessionID)
	}
}

func TestDispatchUnknownProject(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, err := s.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{Project: "ghost", Text: "hi"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestDispatchInjectsTheProjectSubscriptionToken(t *testing.T) {
	runner := &model.FakeRunner{Reply: "ok"}
	s := controlplane.NewServer(runner, &sandbox.FakeProvider{}, secrets.NewMemory())
	ctx := context.Background()

	project, _ := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
	pid := project.GetProject().GetId()

	if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{Project: pid, Key: model.ClaudeCodeOAuthTokenEnv, Value: "tok-xyz"}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "hello"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if got := runner.LastReq.Env[model.ClaudeCodeOAuthTokenEnv]; got != "tok-xyz" {
		t.Fatalf("turn env[%s] = %q, want tok-xyz", model.ClaudeCodeOAuthTokenEnv, got)
	}
}

func TestDispatchWithoutASecretRunsWithNoExtraEnv(t *testing.T) {
	runner := &model.FakeRunner{Reply: "ok"}
	s := newServer(runner)
	ctx := context.Background()

	project, _ := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project.GetProject().GetId(), Text: "hello"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if len(runner.LastReq.Env) != 0 {
		t.Fatalf("turn env = %v, want empty when no secret is set", runner.LastReq.Env)
	}
}

func TestSetSecretStoresValue(t *testing.T) {
	store := secrets.NewMemory()
	s := controlplane.NewServer(&model.FakeRunner{}, &sandbox.FakeProvider{}, store)
	ctx := context.Background()

	project, _ := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
	pid := project.GetProject().GetId()

	if _, err := s.SetSecret(ctx, &quaycrewv1.SetSecretRequest{Project: pid, Key: "token", Value: "s3cret"}); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	got, err := store.Get(ctx, pid, "token")
	if err != nil || got != "s3cret" {
		t.Fatalf("secret not stored: got %q err %v", got, err)
	}
}

func TestSessionSandboxLifecycle(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s := controlplane.NewServer(&model.FakeRunner{Reply: "ok"}, provider, secrets.NewMemory())
	ctx := context.Background()

	project, _ := s.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
	pid := project.GetProject().GetId()

	// Two turns on the same thread must share one sandbox (created once, not per turn).
	first, _ := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, Text: "one"})
	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: pid, ThreadId: first.GetThreadId(), Text: "two"}); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	if len(provider.Created) != 1 {
		t.Fatalf("expected 1 sandbox for the session, got %d (%v)", len(provider.Created), provider.Created)
	}
	if provider.Created[0] != first.GetSessionId() {
		t.Fatalf("sandbox created for %q, want session %q", provider.Created[0], first.GetSessionId())
	}

	// Stopping the session tears its sandbox down.
	if _, err := s.StopSession(ctx, &quaycrewv1.StopSessionRequest{Id: first.GetSessionId()}); err != nil {
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
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
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

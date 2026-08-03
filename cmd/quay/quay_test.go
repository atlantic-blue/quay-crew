package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func testClient(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	srv := controlplane.NewServer(store.NewMemory(), &model.FakeRunner{Reply: "ok"}, &sandbox.FakeProvider{}, secrets.NewMemory())
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, srv)
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
	return quaycrewv1.NewControlPlaneServiceClient(conn)
}

func TestProjectCreateAndList(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	var out bytes.Buffer
	if err := run(ctx, client, []string{"project", "create", "acme"}, &out); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out.String(), "created project") || !strings.Contains(out.String(), "(acme)") {
		t.Fatalf("create output: %q", out.String())
	}

	out.Reset()
	if err := run(ctx, client, []string{"project", "list"}, &out); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out.String(), "acme") {
		t.Fatalf("list output: %q", out.String())
	}
}

func TestDispatch(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	created, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	pid := created.GetProject().GetId()

	var out bytes.Buffer
	if err := run(ctx, client, []string{"dispatch", "--project", pid, "hello", "world"}, &out); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("dispatch reply not shown: %q", out.String())
	}
	if !strings.Contains(out.String(), "session ") || !strings.Contains(out.String(), "thread ") {
		t.Fatalf("dispatch did not show session/thread: %q", out.String())
	}

	out.Reset()
	if err := run(ctx, client, []string{"sessions", "--project", pid}, &out); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !strings.Contains(out.String(), "project="+pid) {
		t.Fatalf("sessions output: %q", out.String())
	}
}

func TestSecretSet(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	created, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	pid := created.GetProject().GetId()

	var out bytes.Buffer
	if err := run(ctx, client, []string{"secret", "set", "--project", pid, "CLAUDE_CODE_OAUTH_TOKEN", "tok-123"}, &out); err != nil {
		t.Fatalf("secret set: %v", err)
	}
	if !strings.Contains(out.String(), "set secret CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("secret set output: %q", out.String())
	}
	// The value must never be echoed back.
	if strings.Contains(out.String(), "tok-123") {
		t.Fatalf("secret value was printed: %q", out.String())
	}
}

func TestSecretSetRequiresProjectKeyValue(t *testing.T) {
	client := testClient(t)
	if err := run(context.Background(), client, []string{"secret", "set", "CLAUDE_CODE_OAUTH_TOKEN", "tok"}, io.Discard); err == nil {
		t.Fatal("secret set without --project = nil error, want error")
	}
	if err := run(context.Background(), client, []string{"secret", "set", "--project", "p1", "only-key"}, io.Discard); err == nil {
		t.Fatal("secret set without a value = nil error, want error")
	}
}

func TestDispatchRequiresProject(t *testing.T) {
	client := testClient(t)
	var out bytes.Buffer
	if err := run(context.Background(), client, []string{"dispatch", "hi"}, &out); err == nil {
		t.Fatal("dispatch without --project = nil error, want error")
	}
}

func TestUnknownCommand(t *testing.T) {
	if err := run(context.Background(), testClient(t), []string{"bogus"}, io.Discard); err == nil {
		t.Fatal("unknown command = nil error, want error")
	}
}

// TestDispatchAcceptsAProjectName covers the papercut this exists for: the operator types the name
// they gave the project, not the hex id printed once at creation.
func TestDispatchAcceptsAProjectName(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	var created bytes.Buffer
	if err := run(ctx, client, []string{"project", "create", "demo"}, &created); err != nil {
		t.Fatalf("project create: %v", err)
	}

	var out bytes.Buffer
	if err := run(ctx, client, []string{"dispatch", "--project", "demo", "hello"}, &out); err != nil {
		t.Fatalf("dispatch by name: %v", err)
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("dispatch by name returned %q, want the reply", out.String())
	}
}

func TestSessionsAcceptsAProjectName(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	var discard bytes.Buffer
	if err := run(ctx, client, []string{"project", "create", "demo"}, &discard); err != nil {
		t.Fatalf("project create: %v", err)
	}
	if err := run(ctx, client, []string{"dispatch", "--project", "demo", "hello"}, &discard); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	var out bytes.Buffer
	if err := run(ctx, client, []string{"sessions", "--project", "demo"}, &out); err != nil {
		t.Fatalf("sessions by name: %v", err)
	}
	if strings.Contains(out.String(), "no sessions") {
		t.Fatalf("sessions by name found nothing: %q", out.String())
	}
}

func TestDispatchRejectsAnUnknownProjectReference(t *testing.T) {
	client := testClient(t)
	var out bytes.Buffer
	err := run(context.Background(), client, []string{"dispatch", "--project", "ghost", "hello"}, &out)
	if err == nil {
		t.Fatal("dispatch to an unknown project succeeded")
	}
	// The operator needs to know it was the project reference that was wrong.
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("the error %q does not name the reference the operator typed", err)
	}
}

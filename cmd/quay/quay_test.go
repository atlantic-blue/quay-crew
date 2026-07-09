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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func testClient(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	srv := controlplane.NewServer(&model.FakeRunner{Reply: "ok"}, &sandbox.FakeProvider{}, secrets.NewMemory())
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

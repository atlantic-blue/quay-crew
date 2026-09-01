//go:build integration

package controlplane_test

import (
	"context"
	"net"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/headroom"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// The headroom answer joins two things that live in two places: what the daemon says a container
// holds, and what the system's own row says the session beside it is doing. The second half is a real
// store read, and the memory store cannot speak for how Postgres stamps `updated_at`.
//
// So this runs the whole path: a real Postgres, the real control plane, the real gRPC interface, and
// a client that asks the way the console asks. The daemon is the one double, because a test cannot
// make a machine run out of memory on purpose.
func TestTheHeadroomAnswerJoinsTheDaemonToTheRealStore(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("quaycrew"),
		postgres.WithUsername("quaycrew"),
		postgres.WithPassword("quaycrew"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		timeout := 30 * time.Second
		_ = container.Stop(context.Background(), &timeout)
	})

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	durable, err := store.NewPostgres(ctx, url)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(durable.Close)

	source := &countedSource{}
	server := controlplane.NewServer(controlplane.Config{
		Store: durable, Runner: &model.FakeRunner{Reply: "done"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Headroom: source, HeadroomEvery: time.Hour,
	})

	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	quaycrewv1.RegisterControlPlaneServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := quaycrewv1.NewControlPlaneServiceClient(conn)

	workspace, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	dispatched, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "read the repository",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// The daemon reports a container for that session, and one the system holds no row for.
	sample := theMachine()
	sample.Sandboxes = []headroom.Sandbox{
		{Session: "a00d36d6454a3de66d02c6a3", Held: headroom.Measured(2 * mebibyte)},
		{
			Session:   dispatched.GetId(),
			Held:      headroom.Measured(1201 * mebibyte),
			Processor: headroom.MeasuredShare(42.5),
		},
	}
	source.mu.Lock()
	source.sample = sample
	source.mu.Unlock()
	server.SampleHeadroom(ctx)

	answer, err := client.GetHeadroom(ctx, &quaycrewv1.GetHeadroomRequest{})
	if err != nil {
		t.Fatalf("GetHeadroom: %v", err)
	}
	boxes := answer.GetSandboxes()
	if len(boxes) != 2 {
		t.Fatalf("%d sandboxes came back, want two", len(boxes))
	}
	// Largest first, whatever order the daemon listed them in.
	if boxes[0].GetSession() != dispatched.GetId() {
		t.Fatalf("the first line is %s, want the largest", boxes[0].GetSession())
	}
	// This is the half the memory store cannot speak for: the status and the age come from a row
	// Postgres wrote and stamped.
	if boxes[0].GetStatus() != controlplane.StatusIdle {
		t.Fatalf("the session reads %q, and the row in Postgres says what it is doing", boxes[0].GetStatus())
	}
	if boxes[0].GetIdle() == "" {
		t.Fatal("no line says how long since the last task, so nothing says which session to stop")
	}
	if boxes[1].GetStatus() != "" {
		t.Fatalf("the system claims a container it holds no row for is %q", boxes[1].GetStatus())
	}

	// And the figures survived the wire whole.
	if answer.GetLimit() != "7837 MiB" || answer.GetState() != headroom.StateRoom {
		t.Fatalf("the system says %s of %s, %s", answer.GetUsed(), answer.GetLimit(), answer.GetState())
	}

	// The daemon was read once, by the sampler, and never by the call.
	if source.count() != 1 {
		t.Fatalf("the machine was read %d times, want once", source.count())
	}
}

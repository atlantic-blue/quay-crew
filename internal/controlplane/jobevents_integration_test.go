//go:build integration

package controlplane_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// The whole path, with nothing stood in for but the model: a real Postgres, a real Redpanda, the
// real control plane and the real gRPC interface.
//
// The behaviour scenarios prove the same shape against an in memory log, which cannot answer whether
// a real broker takes the topic, the key and the bytes. This one can, and it is where the rule lives
// that the row is written in a transaction and the export follows it.
func TestAMovementLandsInPostgresAndOnARealBroker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	durable := aRealStore(t, ctx)
	log, err := messaging.NewClient(seedBroker)
	if err != nil {
		t.Fatalf("event log: %v", err)
	}
	t.Cleanup(log.Close)

	server := controlplane.NewServer(controlplane.Config{
		Store: durable, Runner: &model.FakeRunner{Reply: "the bill is due on the 14th"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(), Events: log,
	})
	client := servedOver(t, server)

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

	declared, err := client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project.GetProject().GetId(),
		Title:   "read the electricity bill", Brief: "open the bill and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	declaredJob := declared.GetJob()
	if len(declaredJob.GetTraceId()) != 32 {
		t.Fatalf("the job traces %q, which joins to nothing", declaredJob.GetTraceId())
	}

	// The controller runs it, so the whole set of movements is written and exported.
	server.TickJob(ctx)
	found, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declaredJob.GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.GetJob().GetPhase() != job.PhaseRunning {
		t.Fatalf("the job is %q after a tick", found.GetJob().GetPhase())
	}
	// The trace survived the row being written and read back through Postgres, which is what makes
	// it useful after the process that declared it has gone.
	if found.GetJob().GetTraceId() != declaredJob.GetTraceId() {
		t.Fatalf("the job reads back tracing %q, want %q",
			found.GetJob().GetTraceId(), declaredJob.GetTraceId())
	}

	// The controller lets go of its task, so the record of it is written by the goroutine running it.
	// Waited for rather than slept on: this is the same wait the system's own shutdown does.
	waiting, doneWaiting := context.WithTimeout(ctx, 30*time.Second)
	server.WaitForTasks(waiting)
	doneWaiting()

	// The task the controller sent carries the same identifier, which is the join issue 346 asks for.
	tasks, err := client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: found.GetJob().GetSession()})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.GetTasks()) == 0 {
		t.Fatal("no task was recorded against the session that ran the job")
	}
	for _, task := range tasks.GetTasks() {
		if task.GetTraceId() != declaredJob.GetTraceId() {
			t.Fatalf("the task row traces %q and the job traces %q, so the two do not join",
				task.GetTraceId(), declaredJob.GetTraceId())
		}
	}

	// And the records are on the real broker, keyed by the job.
	read, cancelRead := context.WithTimeout(ctx, 60*time.Second)
	defer cancelRead()
	kinds := map[string]bool{}
	err = log.Consume(read, "job-events-test", []string{"acme.job"},
		func(_ context.Context, record messaging.Record) error {
			if string(record.Key) != declaredJob.GetId() {
				t.Errorf("a record is keyed by %q, and one job's records share a partition",
					string(record.Key))
			}
			var event quaycrewv1.JobEvent
			if err := proto.Unmarshal(record.Value, &event); err != nil {
				t.Errorf("a record does not decode as a job event: %v", err)
				return nil
			}
			if event.GetTraceId() != declaredJob.GetTraceId() {
				t.Errorf("a %s record traces %q, want the job's own", event.GetKind(), event.GetTraceId())
			}
			kinds[event.GetKind()] = true
			if kinds[job.EventDeclared] && kinds[job.EventClaimed] && kinds[job.EventStarted] {
				cancelRead()
			}
			return nil
		})
	if err != nil && !strings.Contains(err.Error(), "context") {
		t.Fatalf("reading the job stream back: %v", err)
	}
	for _, want := range []string{job.EventDeclared, job.EventClaimed, job.EventStarted} {
		if !kinds[want] {
			t.Fatalf("the broker never carried %q, it carried %v", want, kinds)
		}
	}
}

// A broker that is not there costs the export and nothing else. This is the same rule the unit test
// pins, against a real Postgres: the transaction still commits and the history is whole.
func TestABrokerThatIsNotThereDoesNotCostTheRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	durable := aRealStore(t, ctx)
	// A broker at an address nothing is listening on. The producer connects lazily, so this is a
	// system that believes it has a log and does not.
	log, err := messaging.NewClient("127.0.0.1:1")
	if err != nil {
		t.Fatalf("event log: %v", err)
	}
	t.Cleanup(log.Close)

	server := controlplane.NewServer(controlplane.Config{
		Store: durable, Runner: &model.FakeRunner{Reply: "done"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(), Events: log,
		// A short budget, because this test is about a broker that never answers and the export is
		// bounded so it can never hold the caller.
		ExportWait: 2 * time.Second,
	})
	client := servedOver(t, server)

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

	declared, err := client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project.GetProject().GetId(),
		Title:   "read the electricity bill", Brief: "open the bill and say when it is due",
	})
	if err != nil {
		t.Fatalf("a broker that is not there failed the job: %v", err)
	}

	events, err := durable.ListJobEvents(ctx, declared.GetJob().GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != job.EventDeclared {
		t.Fatalf("%d records are in Postgres after an export nobody took", len(events))
	}
	if events[0].TraceID != declared.GetJob().GetTraceId() {
		t.Fatalf("the record traces %q and the job traces %q", events[0].TraceID, declared.GetJob().GetTraceId())
	}
}

// aRealStore is a Postgres of this test's own, migrated.
func aRealStore(t *testing.T, ctx context.Context) *store.Postgres {
	t.Helper()
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
	return durable
}

// servedOver puts the system behind the real gRPC interface, which is the one every caller speaks.
func servedOver(t *testing.T, server *controlplane.Server) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
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
	return quaycrewv1.NewControlPlaneServiceClient(conn)
}

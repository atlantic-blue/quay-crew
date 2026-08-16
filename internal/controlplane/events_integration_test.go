//go:build integration

package controlplane_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"google.golang.org/protobuf/proto"
)

// seedBroker addresses the Redpanda container shared by every test in this file.
var seedBroker string

// TestMain starts one real Redpanda for the package.
//
// The behaviour scenarios prove a task is published, against an in memory log. This proves the same
// thing survives a real broker: the topic is created, the record is accepted, the key is the session
// and the bytes decode back into the event that was sent. A double cannot answer any of those.
func TestMain(m *testing.M) {
	code, err := runWithRedpanda(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "redpanda: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runWithRedpanda(m *testing.M) (int, error) {
	ctx := context.Background()

	container, err := redpanda.Run(ctx, "redpandadata/redpanda:v24.2.7")
	if err != nil {
		return 0, fmt.Errorf("start: %w", err)
	}
	defer func() {
		timeout := 30 * time.Second
		_ = container.Stop(context.Background(), &timeout)
	}()

	seedBroker, err = container.KafkaSeedBroker(ctx)
	if err != nil {
		return 0, fmt.Errorf("seed broker: %w", err)
	}
	return m.Run(), nil
}

func TestATaskLandsOnARealBroker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log, err := messaging.NewClient(seedBroker)
	if err != nil {
		t.Fatalf("event log: %v", err)
	}
	defer log.Close()

	runner, err := model.NewRunner("echo", "", "")
	if err != nil {
		t.Fatalf("model runner: %v", err)
	}
	server := controlplane.NewServer(controlplane.Config{
		Store:    store.NewMemory(),
		Runner:   runner,
		Provider: &sandbox.FakeProvider{},
		Secrets:  secrets.NewMemory(),
		Events:   log,
	})

	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "say pong",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	topic, err := messaging.Topic("acme", "tasks")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}

	received := make(chan messaging.Record, 1)
	consumeCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	go func() {
		_ = log.Consume(consumeCtx, "task-events-test", []string{topic}, func(_ context.Context, record messaging.Record) error {
			select {
			case received <- record:
			default:
			}
			stop()
			return nil
		})
	}()

	select {
	case record := <-received:
		if got, want := string(record.Key), dispatched.GetId(); got != want {
			t.Errorf("the record is keyed %q, want the session %q", got, want)
		}
		event := &quaycrewv1.TaskEvent{}
		if err := proto.Unmarshal(record.Value, event); err != nil {
			t.Fatalf("the record on %s does not decode as a task event: %v", topic, err)
		}
		if event.GetPrompt() != "say pong" {
			t.Errorf("the record says %q was asked, want %q", event.GetPrompt(), "say pong")
		}
		if got, want := event.GetReply(), dispatched.GetReply(); got != want {
			t.Errorf("the record carries reply %q, want the one the caller got, %q", got, want)
		}
		if event.GetStatus() != "idle" {
			t.Errorf("the record says the session is %q, want idle", event.GetStatus())
		}
		if event.GetSession() != dispatched.GetId() {
			t.Errorf("the record names session %q, want %q", event.GetSession(), dispatched.GetId())
		}
	case <-consumeCtx.Done():
		t.Fatalf("no task arrived on %s within the timeout, so nothing was published to the broker", topic)
	}
}

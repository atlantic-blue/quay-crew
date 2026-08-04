//go:build integration

package controlplane_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/projection"
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
// The behaviour scenarios prove a turn is published, against an in memory log. This proves the same
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

func TestATurnLandsOnARealBroker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log, err := messaging.NewClient(seedBroker)
	if err != nil {
		t.Fatalf("event log: %v", err)
	}
	defer log.Close()

	runner, err := model.NewRunner("echo", "")
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

	topic, err := messaging.Topic("acme", "turns")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}

	received := make(chan messaging.Record, 1)
	consumeCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	go func() {
		_ = log.Consume(consumeCtx, "turn-events-test", []string{topic}, func(_ context.Context, record messaging.Record) error {
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
		if got, want := string(record.Key), dispatched.GetSessionId(); got != want {
			t.Errorf("the record is keyed %q, want the session %q", got, want)
		}
		event := &quaycrewv1.TurnEvent{}
		if err := proto.Unmarshal(record.Value, event); err != nil {
			t.Fatalf("the record on %s does not decode as a turn event: %v", topic, err)
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
		if event.GetSession() != dispatched.GetSessionId() {
			t.Errorf("the record names session %q, want %q", event.GetSession(), dispatched.GetSessionId())
		}
	case <-consumeCtx.Done():
		t.Fatalf("no turn arrived on %s within the timeout, so nothing was published to the broker", topic)
	}
}

// TestTheProjectionReadsTurnsBackFromARealBroker proves the half a double cannot.
//
// The behaviour scenarios drive the projection against an in memory log with a topic list it
// already knows. This one subscribes by regular expression to a topic that did not exist when the
// consumer started, against a real broker, which is the arrangement the running crew uses and the
// one where a wrong pattern or a missing option silently reads nothing forever.
func TestTheProjectionReadsTurnsBackFromARealBroker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log, err := messaging.NewClient(seedBroker)
	if err != nil {
		t.Fatalf("event log: %v", err)
	}
	defer log.Close()

	runner, err := model.NewRunner("echo", "")
	if err != nil {
		t.Fatalf("model runner: %v", err)
	}
	durable := store.NewMemory()
	server := controlplane.NewServer(controlplane.Config{
		Store:    durable,
		Runner:   runner,
		Provider: &sandbox.FakeProvider{},
		Secrets:  secrets.NewMemory(),
		Events:   log,
	})

	// A workspace named for this test, so its stream is one no consumer has seen before.
	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "projected"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	first, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project.GetProject().GetId(), Text: "hello"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), ThreadId: first.GetThreadId(), Text: "and again",
	}); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}

	projectionCtx, stopProjection := context.WithCancel(ctx)
	defer stopProjection()
	go func() {
		_ = projection.New(log, durable, slog.New(slog.NewTextHandler(io.Discard, nil))).Run(projectionCtx)
	}()

	// The projection is a consumer of a live broker, so this waits for it rather than assuming.
	deadline := time.Now().Add(60 * time.Second)
	for {
		listed, err := server.ListTurns(ctx, &quaycrewv1.ListTurnsRequest{Session: first.GetSessionId()})
		if err != nil {
			t.Fatalf("list turns: %v", err)
		}
		if len(listed.GetTurns()) == 2 {
			if listed.GetTurns()[0].GetPrompt() != "hello" || listed.GetTurns()[1].GetPrompt() != "and again" {
				t.Fatalf("the history came back as %q then %q, want hello then and again",
					listed.GetTurns()[0].GetPrompt(), listed.GetTurns()[1].GetPrompt())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d turns were projected within the timeout, want 2: the consumer is not reading the stream",
				len(listed.GetTurns()))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

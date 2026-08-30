package controlplane_test

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/headroom"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// The room view's summary over the whole path: the real control plane, the real gRPC interface, a
// real session in the store, and the console's own resource.
//
// The table tests in internal/console prove how the line is written from an answer. What they cannot
// answer is whether it is the crew's actual reading: they build it from a response a case wrote. So
// this gives the crew a machine, dispatches work into it, and reads the line the operator would be
// looking at.
func TestTheRoomViewSummarisesTheMachineTheCrewActuallyRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	machine := &aReadableMachine{sample: aMachineHolding(3628, 7837)}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "it is due on the 14th"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Headroom: machine, HeadroomEvery: time.Hour,
	})
	client := roomServedOver(t, server)
	room := theRoomView(t, client)

	// A session of the crew's own, so the listing under the line is joined to a real row rather than
	// to a container nobody holds.
	session := aDispatchedSession(t, ctx, client)
	machine.holding(aMachineHolding(3628, 7837), headroom.Sandbox{
		Session: session, Held: headroom.Measured(1201 * mebibyte),
		Processor: headroom.MeasuredShare(42.5),
	})
	server.SampleHeadroom(ctx)

	line, state := room.Summary(ctx, "")
	for _, want := range []string{"3628 MiB", "7837 MiB", "4209 MiB", headroom.StateRoom} {
		if !strings.Contains(line, want) {
			t.Errorf("the summary does not carry %q:\n%s", want, line)
		}
	}
	if state != console.StateReady {
		t.Errorf("a machine with room is drawn as %v, want %v", state, console.StateReady)
	}

	// The line is above the table and not in it. A summary row would count as a sandbox, so eighteen
	// of them would read nineteen on the panel's edge and the cursor could land on a row that is not
	// a session.
	rows, err := room.List(ctx, "")
	if err != nil {
		t.Fatalf("listing the room view: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the machine holds one sandbox and the view lists %d rows", len(rows))
	}
}

// The word turns on the crew's next reading rather than on whatever it read when the console opened.
func TestTheSummaryTurnsWhenTheMachineFills(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	machine := &aReadableMachine{sample: aMachineHolding(3628, 7837)}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Headroom: machine, HeadroomEvery: time.Hour,
	})
	client := roomServedOver(t, server)
	room := theRoomView(t, client)
	server.SampleHeadroom(ctx)

	if _, state := room.Summary(ctx, ""); state != console.StateReady {
		t.Fatalf("a machine holding 3628 of 7837 is drawn as %v, and this case is about it turning", state)
	}

	machine.holding(aMachineHolding(7200, 7837))
	server.SampleHeadroom(ctx)

	line, state := room.Summary(ctx, "")
	if state != console.StateFailed {
		t.Errorf("a machine holding 7200 of 7837 is drawn as %v, want %v", state, console.StateFailed)
	}
	if !strings.Contains(line, strings.ToUpper(headroom.StateFull)) {
		t.Errorf("the summary does not say the machine is full:\n%s", line)
	}
	if !strings.Contains(line, "not enough for another sandbox") {
		t.Errorf("the summary does not say another sandbox will not fit:\n%s", line)
	}
}

// A crew whose machine cannot be read says so on the line. It is the one answer that must never come
// back as healthy: the header that drew a healthy crew through eighteen kills is why this view
// exists. See issue 405.
func TestTheSummarySaysUnknownWhereTheCrewCouldNotReadTheMachine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	machine := &aReadableMachine{err: errDaemonSilent}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Headroom: machine, HeadroomEvery: time.Hour,
	})
	client := roomServedOver(t, server)
	room := theRoomView(t, client)
	server.SampleHeadroom(ctx)

	line, state := room.Summary(ctx, "")
	if state == console.StateReady {
		t.Errorf("a machine nobody could read is drawn as healthy:\n%s", line)
	}
	for _, want := range []string{headroom.StateUnknown, errDaemonSilent.Error()} {
		if !strings.Contains(line, want) {
			t.Errorf("the summary does not carry %q:\n%s", want, line)
		}
	}
}

var errDaemonSilent = &daemonSilent{}

type daemonSilent struct{}

func (*daemonSilent) Error() string { return "the daemon is not answering" }

// aReadableMachine is a machine the crew is given, so a case can be a crew on a full machine without
// filling one. It can be given a different reading part way through, which is what a machine filling
// up under a console looks like.
type aReadableMachine struct {
	mu     sync.Mutex
	sample headroom.Sample
	err    error
}

func (m *aReadableMachine) Sample(context.Context) (headroom.Sample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sample, m.err
}

func (m *aReadableMachine) holding(sample headroom.Sample, boxes ...headroom.Sandbox) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sample.Sandboxes = boxes
	m.sample, m.err = sample, nil
}

// aMachineHolding is one reading, in mebibytes, written the way the incident of 27 August 2026
// recorded it.
func aMachineHolding(used, limit int64) headroom.Sample {
	return headroom.Sample{
		Used: headroom.Measured(used * mebibyte), Limit: headroom.Measured(limit * mebibyte),
		TakenAt: time.Now(),
	}
}

// theRoomView is the console's own room resource over this crew, so the case reads what the operator
// reads rather than a copy of it.
func theRoomView(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) console.Resource {
	t.Helper()
	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("building the console: %v", err)
	}
	room, found := registry.Get("room")
	if !found {
		t.Fatal("the console has no room view")
	}
	if room.Summary == nil {
		t.Fatal("the room view has no summary, so it still says nothing about the machine")
	}
	return room
}

// aDispatchedSession puts one real session in the crew and answers with its identifier.
func aDispatchedSession(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) string {
	t.Helper()
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
		Project: project.GetProject().GetId(), Text: "when is the electricity bill due",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	return dispatched.GetId()
}

// roomServedOver puts the crew behind a real gRPC connection, because the console only ever talks to
// one through a client.
func roomServedOver(t *testing.T, server *controlplane.Server) quaycrewv1.ControlPlaneServiceClient {
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

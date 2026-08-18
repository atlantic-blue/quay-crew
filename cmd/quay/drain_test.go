package main

import (
	"bytes"
	"context"
	"errors"
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

func TestDrainSaysWhichSessionsItPutDown(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	handle := onlySession(t, client).GetHandle()[:8]

	said := mustRun(t, client, "drain")

	if !strings.Contains(said, handle) {
		t.Fatalf("the drain does not name the session it stopped: %q", said)
	}
	if !strings.Contains(said, "1 session is down") {
		t.Fatalf("the drain does not say how much went down: %q", said)
	}
	// A count of one that reads as a plural is the line that makes an operator doubt the rest.
	if strings.Contains(said, "1 sessions") {
		t.Fatalf("the drain counts one session as a plural: %q", said)
	}
}

func TestDrainOnACrewWithNothingLiveSaysSo(t *testing.T) {
	client := testClient(t)

	said := mustRun(t, client, "drain")

	if !strings.Contains(said, "nothing to put down") {
		t.Fatalf("a drain with nothing live said %q", said)
	}
}

// The command has no other word, and a mistyped one has to fail rather than be taken for the drain
// the operator did not ask for.
func TestDrainTakesOnlyTheWordAnyway(t *testing.T) {
	client := testClient(t)

	var out bytes.Buffer
	err := run(context.Background(), client, []string{"drain", "everything"}, &out, "")
	if err == nil {
		t.Fatalf("quay drain everything was accepted, and it drained: %q", out.String())
	}
	if !strings.Contains(err.Error(), "anyway") {
		t.Fatalf("the refusal does not say the word it takes: %v", err)
	}
}

// This tool has no flags, so the force spelling is a word. The flag shape has to fail rather than be
// read as an address or ignored.
func TestDrainRefusesTheFlagSpelling(t *testing.T) {
	client := testClient(t)

	var out bytes.Buffer
	if err := run(context.Background(), client, []string{"drain", "--force"}, &out, ""); err == nil {
		t.Fatalf("quay drain --force was accepted: %q", out.String())
	}
}

// The refusal is the whole point of the plain word: an operator who cannot see whose task is working
// cannot decide whether to wait for it.
func TestDrainRefusesWhileATaskIsWorkingAndNamesIt(t *testing.T) {
	client, handle := aCrewWithATaskUnderWay(t)

	var out bytes.Buffer
	err := run(context.Background(), client, []string{"drain"}, &out, "")
	if err == nil {
		t.Fatalf("the drain went ahead over a task that was still working: %q", out.String())
	}
	if !strings.Contains(err.Error(), handle) {
		t.Fatalf("the refusal does not name the session that is working: %v", err)
	}
	if !strings.Contains(err.Error(), "1 task is") {
		t.Fatalf("the refusal does not count the tasks in flight: %v", err)
	}
}

func TestDrainAnywaySaysWhatItInterrupted(t *testing.T) {
	client, handle := aCrewWithATaskUnderWay(t)

	said := mustRun(t, client, "drain", "anyway")

	if !strings.Contains(said, "was working, and that task is gone") {
		t.Fatalf("draining anyway said nothing about the task it took: %q", said)
	}
	if !strings.Contains(said, handle) {
		t.Fatalf("draining anyway does not name whose task went: %q", said)
	}
}

// aCrewWithATaskUnderWay is a crew with one session the store calls running, which is what a task in
// flight looks like to anything asking. The model double answers at once, so the status is set here
// rather than by holding a task open, which a command line test has no way to do.
func aCrewWithATaskUnderWay(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, string) {
	t.Helper()
	memory := store.NewMemory()
	client := testClientWith(t, controlplane.Config{
		Store: memory, Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "hello")

	session := onlySession(t, client)
	if err := memory.RecordTask(context.Background(), session.GetId(), "", "running"); err != nil {
		t.Fatalf("mark the session running: %v", err)
	}
	return client, session.GetHandle()[:8]
}

// A crew that predates this command answers Unimplemented, and it answers that during the very
// upgrade that installs the answer: `make upgrade` builds the tool before it rebuilds the stack, so
// the new binary talks to the old crew exactly once. Failing there would block the upgrade forever.
func TestDrainCarriesOnAgainstACrewFromBeforeIt(t *testing.T) {
	client := clientOfACrewThatServesNothing(t)

	var out bytes.Buffer
	if err := run(context.Background(), client, []string{"drain"}, &out, ""); err != nil {
		t.Fatalf("drain failed against an older crew, which would block the upgrade: %v", err)
	}
	if !strings.Contains(out.String(), "from before draining") {
		t.Fatalf("the drain does not say why it could not put anything down: %q", out.String())
	}
}

// A crew that is not up runs no tasks, so there is nothing to lose and nothing to refuse. Failing
// here would stop an upgrade on a machine whose stack is simply down, which is most of them.
func TestDrainCarriesOnWhenTheCrewIsNotUp(t *testing.T) {
	client := clientOfACrewThatIsNotUp(t)

	var out bytes.Buffer
	if err := run(context.Background(), client, []string{"drain"}, &out, ""); err != nil {
		t.Fatalf("drain failed against a crew that is not up: %v", err)
	}
	if !strings.Contains(out.String(), "not up") {
		t.Fatalf("the drain does not say the crew is not up: %q", out.String())
	}
}

// clientOfACrewThatServesNothing dials a real gRPC server with no service registered, which is what
// an older crew looks like from here: it answers, and it answers Unimplemented.
func clientOfACrewThatServesNothing(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)
	return clientOver(t, func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) })
}

// clientOfACrewThatIsNotUp dials nothing at all.
func clientOfACrewThatIsNotUp(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	return clientOver(t, func(context.Context, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	})
}

func clientOver(t *testing.T, dial func(context.Context, string) (net.Conn, error)) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dial),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return quaycrewv1.NewControlPlaneServiceClient(conn)
}

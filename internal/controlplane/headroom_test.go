package controlplane_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/headroom"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// countedSource records how often the system read the machine, so a test can say the daemon was not
// reached rather than infer it from a figure that could have come from anywhere.
type countedSource struct {
	mu     sync.Mutex
	calls  int
	sample headroom.Sample
	err    error
}

func (c *countedSource) Sample(context.Context) (headroom.Sample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.sample, c.err
}

func (c *countedSource) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

const mebibyte = int64(1 << 20)

// theMachineOn 27 August 2026, as the incident recorded it: the daemon at 3,628 megabytes of a
// 7,837 megabyte cap, and the machine underneath it at 94 per cent of its swap.
func theMachine() headroom.Sample {
	return headroom.Sample{
		Used:  headroom.Measured(3628 * mebibyte),
		Limit: headroom.Measured(7837 * mebibyte),
		Machine: headroom.Machine{
			Name:      "Docker Desktop",
			Total:     headroom.Measured(7837 * mebibyte),
			Available: headroom.Measured(1503 * mebibyte),
			SwapTotal: headroom.Measured(17408 * mebibyte),
			SwapUsed:  headroom.Measured(16402 * mebibyte),
		},
		TakenAt: time.Now(),
	}
}

func systemReading(t *testing.T, source headroom.Source) *controlplane.Server {
	t.Helper()
	return controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Headroom: source, HeadroomEvery: time.Hour,
	})
}

// Rule one of issue 405. The header asks this every second, so it has to answer from the last
// sample. A call that read the daemon would put a `docker stats` on the redraw path.
func TestTheHeadroomCallNeverReadsTheMachine(t *testing.T) {
	source := &countedSource{sample: theMachine()}
	server := systemReading(t, source)
	ctx := context.Background()

	server.SampleHeadroom(ctx)
	if source.count() != 1 {
		t.Fatalf("one sample read the machine %d times", source.count())
	}

	// A minute of header redraws.
	for i := 0; i < 60; i++ {
		answer, err := server.GetHeadroom(ctx, &quaycrewv1.GetHeadroomRequest{})
		if err != nil {
			t.Fatalf("GetHeadroom: %v", err)
		}
		if answer.GetUsed() != "3628 MiB" {
			t.Fatalf("the system says it holds %q", answer.GetUsed())
		}
	}
	if source.count() != 1 {
		t.Fatalf("sixty calls read the machine %d times, want once", source.count())
	}
}

// Rule two: the limit that binds, named. And the word, which must be readable on its own.
func TestTheSystemReportsTheLimitThatBindsAndOneWordAboutIt(t *testing.T) {
	source := &countedSource{sample: theMachine()}
	server := systemReading(t, source)
	server.SampleHeadroom(context.Background())

	answer, err := server.GetHeadroom(context.Background(), &quaycrewv1.GetHeadroomRequest{})
	if err != nil {
		t.Fatalf("GetHeadroom: %v", err)
	}
	if answer.GetLimit() != "7837 MiB" {
		t.Fatalf("the binding limit reads %q", answer.GetLimit())
	}
	if answer.GetFree() != "4209 MiB" {
		t.Fatalf("free reads %q", answer.GetFree())
	}
	if answer.GetState() != headroom.StateRoom {
		t.Fatalf("3628 of 7837 reads %q", answer.GetState())
	}
	if answer.GetUsedBytes() != 3628*mebibyte || answer.GetLimitBytes() != 7837*mebibyte {
		t.Fatalf("the byte counts read %d and %d", answer.GetUsedBytes(), answer.GetLimitBytes())
	}
	if answer.GetTakenAt() == nil {
		t.Fatal("the answer does not say when it was read")
	}
}

// Rule three. The machine underneath the daemon is reported apart from it, because that is where
// the kill came from while the daemon sat at less than half its cap.
func TestTheSystemReportsTheMachinesPressureApartFromTheDaemons(t *testing.T) {
	source := &countedSource{sample: theMachine()}
	server := systemReading(t, source)
	server.SampleHeadroom(context.Background())

	answer, err := server.GetHeadroom(context.Background(), &quaycrewv1.GetHeadroomRequest{})
	if err != nil {
		t.Fatalf("GetHeadroom: %v", err)
	}
	if answer.GetMachineName() != "Docker Desktop" {
		t.Fatalf("the machine is named %q, and a system must not claim to know a machine it cannot read",
			answer.GetMachineName())
	}
	if answer.GetSwapUsed() != "16402 MiB" || answer.GetSwapTotal() != "17408 MiB" {
		t.Fatalf("swap reads %q of %q", answer.GetSwapUsed(), answer.GetSwapTotal())
	}
	if answer.GetMachineAvailable() != "1503 MiB" {
		t.Fatalf("the machine's free memory reads %q", answer.GetMachineAvailable())
	}
}

// Rules four and five together. A daemon that will not answer leaves every figure unknown, the call
// still works, and nothing anywhere reads as a zero or as room.
func TestADaemonThatWillNotAnswerLeavesUnknownAndFailsNothing(t *testing.T) {
	source := &countedSource{err: fmt.Errorf("the daemon is not answering")}
	server := systemReading(t, source)
	server.SampleHeadroom(context.Background())

	answer, err := server.GetHeadroom(context.Background(), &quaycrewv1.GetHeadroomRequest{})
	if err != nil {
		t.Fatalf("a daemon that would not answer failed the call: %v", err)
	}
	for what, said := range map[string]string{
		"used": answer.GetUsed(), "limit": answer.GetLimit(), "free": answer.GetFree(),
		"the machine's memory": answer.GetMachineTotal(), "swap": answer.GetSwapUsed(),
	} {
		if said != "unknown" {
			t.Fatalf("%s reads %q on a machine nobody could read", what, said)
		}
	}
	if answer.GetState() != headroom.StateUnknown {
		t.Fatalf("the state reads %q, and a system that measured nothing must never say room", answer.GetState())
	}
	if answer.GetUsedBytes() != -1 || answer.GetLimitBytes() != -1 {
		t.Fatalf("the byte counts read %d and %d, and zero bytes is a machine that is empty",
			answer.GetUsedBytes(), answer.GetLimitBytes())
	}
	if !strings.Contains(answer.GetFailed(), "not answering") {
		t.Fatalf("the answer does not say why it knows nothing: %q", answer.GetFailed())
	}

	// And the system still serves everything else, which is what "never fail a command" means.
	if _, err := server.ListWorkspaces(context.Background(), &quaycrewv1.ListWorkspacesRequest{}); err != nil {
		t.Fatalf("a system that could not read its machine stopped serving: %v", err)
	}
}

// A system nobody has sampled yet, which is every system for the first moment of its life.
func TestASystemThatHasNotReadTheMachineSaysSoRatherThanReportingRoom(t *testing.T) {
	server := systemReading(t, &countedSource{sample: theMachine()})
	answer, err := server.GetHeadroom(context.Background(), &quaycrewv1.GetHeadroomRequest{})
	if err != nil {
		t.Fatalf("GetHeadroom: %v", err)
	}
	if answer.GetTakenAt() != nil {
		t.Fatal("a system that never sampled says when it read the machine")
	}
	if answer.GetState() != headroom.StateUnknown {
		t.Fatalf("it says %q", answer.GetState())
	}
}

// The view answers which session to stop, so the largest is first and each line says what its
// session is doing. The largest sandbox may be the one doing the work.
func TestTheSandboxesComeBackLargestFirstAndSayWhatEachSessionIsDoing(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	source := &countedSource{}
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner, Provider: &sandbox.FakeProvider{},
		Secrets: secrets.NewMemory(), Headroom: source, HeadroomEvery: time.Hour,
	})
	ctx := context.Background()
	_, project := newProject(t, server)

	first, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "one"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	second, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "two"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	// The daemon reports the smaller one first, and a stray container the system holds no session for.
	sample := theMachine()
	sample.Sandboxes = []headroom.Sandbox{
		{Session: first.GetId(), Held: headroom.Measured(2 * mebibyte), Processor: headroom.MeasuredShare(0.1)},
		{Session: "a00d36d6454a3de66d02c6a3", Held: headroom.Measured(500 * mebibyte)},
		{Session: second.GetId(), Held: headroom.Measured(1201 * mebibyte), Processor: headroom.MeasuredShare(42.5)},
	}
	source.mu.Lock()
	source.sample = sample
	source.mu.Unlock()
	server.SampleHeadroom(ctx)

	answer, err := server.GetHeadroom(ctx, &quaycrewv1.GetHeadroomRequest{})
	if err != nil {
		t.Fatalf("GetHeadroom: %v", err)
	}
	boxes := answer.GetSandboxes()
	if len(boxes) != 3 {
		t.Fatalf("%d sandboxes came back, want three", len(boxes))
	}
	if boxes[0].GetSession() != second.GetId() || boxes[0].GetHeld() != "1201 MiB" {
		t.Fatalf("the first line is %s holding %s, want the largest", boxes[0].GetSession(), boxes[0].GetHeld())
	}
	if boxes[0].GetProcessor() != "42.5%" {
		t.Fatalf("its processor share reads %q", boxes[0].GetProcessor())
	}
	if boxes[0].GetStatus() != controlplane.StatusIdle {
		t.Fatalf("the system says that session is %q, and the row beside the container is what says so",
			boxes[0].GetStatus())
	}
	if boxes[0].GetIdle() == "" {
		t.Fatal("no line says how long since that session's last task, which is what a listing is read for")
	}
	// The stray sits between them by size, and the system says nothing about a session it does not have.
	if boxes[1].GetSession() != "a00d36d6454a3de66d02c6a3" {
		t.Fatalf("the second line is %s", boxes[1].GetSession())
	}
	if boxes[1].GetStatus() != "" {
		t.Fatalf("the system claims a stray container is %q", boxes[1].GetStatus())
	}
	if boxes[2].GetSession() != first.GetId() {
		t.Fatalf("the last line is %s, want the smallest", boxes[2].GetSession())
	}
}

// A sandbox the daemon could not read is reported as unknown and sorted last, rather than reported
// as holding nothing. An operator reads a small figure as safe to leave alone.
func TestASandboxTheDaemonCouldNotReadSaysUnknownAndSortsLast(t *testing.T) {
	source := &countedSource{}
	server := systemReading(t, source)
	sample := theMachine()
	sample.Sandboxes = []headroom.Sandbox{
		{Session: "b11e47e7565b4ef77e13d7b4", Held: headroom.Unknown(), Processor: headroom.UnknownShare()},
		{Session: "a00d36d6454a3de66d02c6a3", Held: headroom.Measured(2 * mebibyte)},
	}
	source.sample = sample
	server.SampleHeadroom(context.Background())

	answer, err := server.GetHeadroom(context.Background(), &quaycrewv1.GetHeadroomRequest{})
	if err != nil {
		t.Fatalf("GetHeadroom: %v", err)
	}
	boxes := answer.GetSandboxes()
	if boxes[1].GetSession() != "b11e47e7565b4ef77e13d7b4" {
		t.Fatalf("the unread sandbox is at position 0, and an unknown figure is not a large one")
	}
	if boxes[1].GetHeld() != "unknown" || boxes[1].GetProcessor() != "unknown" {
		t.Fatalf("it reads %q and %q", boxes[1].GetHeld(), boxes[1].GetProcessor())
	}
	if boxes[1].GetHeldBytes() != -1 {
		t.Fatalf("its byte count reads %d, and zero would read as an empty container", boxes[1].GetHeldBytes())
	}
}

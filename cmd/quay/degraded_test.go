package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// On 29 August 2026 a crew's event log had been dead for sixteen hours. The container health check
// had failed 1,467 times in a row and nothing watched it, so an operator working through this tool
// all day read a crew that looked well. See issue 445.

// probeWait is what a test gives a write that will never land. The measured budget is five seconds,
// and a suite that waits five seconds to watch one is a suite nobody runs.
const probeWait = 200 * time.Millisecond

// part builds one entry of a reading, the way the crew hands it over.
func part(name, state, detail string) *quaycrewv1.HealthComponent {
	return &quaycrewv1.HealthComponent{Name: name, State: state, Detail: detail}
}

// The whole point: the part, and why it is down, in the line itself. "Not serving" without the
// dependency is a fact nobody can act on, which is what the container health check already said.
func TestTheLineNamesThePartThatIsDownAndWhy(t *testing.T) {
	lines := degraded([]*quaycrewv1.HealthComponent{
		part(display.HealthStore, display.HealthServing, ""),
		part(display.HealthEvents, display.HealthDown,
			"the event log did not take a record: unable to dial: lookup redpanda: no such host"),
	})

	if len(lines) != 1 {
		t.Fatalf("%d lines for one dead part: %q", len(lines), lines)
	}
	for _, want := range []string{"not serving", display.HealthEvents, "lookup redpanda"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the line does not say %q: %q", want, lines[0])
		}
	}
	if strings.Contains(lines[0], "\n") {
		t.Errorf("a part that is down takes more than one line: %q", lines[0])
	}
}

// Both, because a crew whose store and log are both gone is a crew where naming one of them sends
// the operator to fix half of it.
func TestEveryPartThatIsDownGetsALine(t *testing.T) {
	lines := degraded([]*quaycrewv1.HealthComponent{
		part(display.HealthStore, display.HealthDown, "the store did not take a write: timeout"),
		part(display.HealthEvents, display.HealthDown, "the event log did not take a record: timeout"),
	})

	if len(lines) != 2 {
		t.Fatalf("%d lines for two dead parts: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], display.HealthStore) || !strings.Contains(lines[1], display.HealthEvents) {
		t.Fatalf("the lines do not name both parts: %q", lines)
	}
}

// A part that is down and says nothing about why still has to be reported, and the line has to send
// the operator somewhere rather than stopping at the word.
func TestAPartThatIsDownWithNoDetailStillSaysWhereToLook(t *testing.T) {
	lines := degraded([]*quaycrewv1.HealthComponent{part(display.HealthStore, display.HealthDown, "")})

	if len(lines) != 1 {
		t.Fatalf("%d lines for a dead part with no detail: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "log") {
		t.Fatalf("the line says nothing about where the reason is: %q", lines[0])
	}
}

// Nothing on every other state. A crew with no event log configured is a real crew, and a part
// nothing probes is the absence of a reading: a warning on either, on every command, forever, is a
// line nobody can act on. The console's stats view is where all four states are said.
func TestNothingIsSaidAboutAPartThatIsNotDown(t *testing.T) {
	for _, state := range []string{
		display.HealthServing, display.HealthNotConfigured, display.HealthNotChecked, "",
	} {
		if lines := degraded([]*quaycrewv1.HealthComponent{
			part(display.HealthEvents, state, "")}); len(lines) != 0 {
			t.Errorf("a part reading %q is reported as down: %q", state, lines)
		}
	}
}

// A crew that has never probed claims nothing about itself, so neither does this.
func TestNothingIsSaidAboutACrewThatHasProbedNothing(t *testing.T) {
	if lines := degraded(nil); len(lines) != 0 {
		t.Fatalf("a crew that has probed nothing is reported as down: %q", lines)
	}
}

// stallingLog takes a record and never answers, which is a broker that accepts the connection and
// says nothing after it. It is a different fault from one that refuses: a refusal comes back.
type stallingLog struct{}

func (stallingLog) Publish(ctx context.Context, _ string, _, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}

func (stallingLog) Consume(context.Context, string, []string, messaging.Handler) error { return nil }

func (stallingLog) ConsumePattern(context.Context, string, string, messaging.Handler) error {
	return nil
}

func (stallingLog) Close() {}

// aCrewWhoseEventLogIsDead is the crew of 29 August 2026: the store takes every write, the broker is
// gone, and the crew has probed itself and knows.
func aCrewWhoseEventLogIsDead(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Events: stallingLog{}, ExportWait: probeWait,
	})
	server.ProbeHealth(context.Background())
	return testClientFor(t, server)
}

// Over the wire, against a real control plane, because the whole finding is that the crew knew and
// the tool never asked.
func TestACommandAgainstADegradedCrewNamesTheDependencyThatIsDown(t *testing.T) {
	client := aCrewWhoseEventLogIsDead(t)

	var said bytes.Buffer
	reportDegraded(context.Background(), client, &said)

	for _, want := range []string{"not serving", display.HealthEvents, "did not take a record"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("standard error does not say %q: %q", want, said.String())
		}
	}
}

// The warning belongs on standard error, and the command still answers. A degraded crew takes reads
// perfectly well, which is exactly why nobody noticed for sixteen hours.
func TestACommandStillAnswersAgainstADegradedCrew(t *testing.T) {
	client := aCrewWhoseEventLogIsDead(t)
	mustRun(t, client, "workspace", "create", "acme")

	listed, err := runQuay(t, client, "workspace", "list")
	if err != nil {
		t.Fatalf("quay workspace list against a degraded crew: %v", err)
	}
	if !strings.Contains(listed, "acme") {
		t.Fatalf("the listing is missing its answer: %q", listed)
	}
	if strings.Contains(listed, "not serving") {
		t.Fatalf("standard output carries the warning, which belongs on standard error: %q", listed)
	}
}

// A crew that writes where a dispatch writes says nothing at all, or the line is on every command
// forever and stops being read.
func TestNothingIsSaidAboutACrewThatCanWrite(t *testing.T) {
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Events: messaging.NewMemory(),
	})
	server.ProbeHealth(context.Background())

	var said bytes.Buffer
	reportDegraded(context.Background(), testClientFor(t, server), &said)

	if said.String() != "" {
		t.Fatalf("standard error says %q about a crew that takes every write", said.String())
	}
}

// The check must never stop a command, and a crew that cannot be reached is the command's own
// business to report.
func TestNothingIsSaidWhenTheCrewCannotBeReached(t *testing.T) {
	var said bytes.Buffer
	reportDegraded(context.Background(), unreachableClient(t), &said)

	if said.String() != "" {
		t.Fatalf("standard error says %q about a crew nobody reached", said.String())
	}
}

package controlplane_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// stalledStore reads the way the real one does and never takes a write, which is the crew this is
// all about: every listing answered and nothing started.
type stalledStore struct {
	store.Store
}

func (stalledStore) Probe(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestACrewThatCanWriteIsServing(t *testing.T) {
	health := controlplane.NewHealth(waitingCrew(controlplane.Config{}))
	answer, err := health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if answer.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("a crew that writes answered %s", answer.GetStatus())
	}
}

// The check has to write, because the crew it exists for answered every read in under a second while
// it started no work at all.
func TestACrewWhoseStoreTakesNoWriteIsNotServing(t *testing.T) {
	health := controlplane.NewHealth(waitingCrew(controlplane.Config{
		Store: stalledStore{Store: store.NewMemory()},
	}))
	answer, err := health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if answer.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("a crew whose store takes no write answered %s", answer.GetStatus())
	}
}

func TestACrewWhoseEventLogNeverAnswersIsNotServing(t *testing.T) {
	health := controlplane.NewHealth(waitingCrew(controlplane.Config{Events: stalledLog{}}))
	answer, err := health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if answer.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("a crew whose event log never answers answered %s", answer.GetStatus())
	}
}

// A crew asked about its one service says the same thing about it.
func TestTheListingOfServicesSaysWhatTheCheckSays(t *testing.T) {
	health := controlplane.NewHealth(waitingCrew(controlplane.Config{}))
	listed, err := health.List(context.Background(), &grpc_health_v1.HealthListRequest{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed.GetStatuses()) != 1 {
		t.Fatalf("%d services came back, want the one this process serves", len(listed.GetStatuses()))
	}
	for name, answer := range listed.GetStatuses() {
		if answer.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
			t.Fatalf("%s answered %s", name, answer.GetStatus())
		}
	}
}

// countingStore takes every write and counts the probes, so a test can say a call did not probe
// rather than infer it from an answer that might have come from anywhere.
type countingStore struct {
	store.Store
	probes atomic.Int64
}

func (c *countingStore) Probe(ctx context.Context) error {
	c.probes.Add(1)
	return c.Store.Probe(ctx)
}

// stateOf is what the reading says about one part, and the empty string when it says nothing at all.
func stateOf(reading controlplane.HealthReading, name string) (string, string) {
	for _, component := range reading.Components {
		if component.Name == name {
			return component.State, component.Detail
		}
	}
	return "", ""
}

// TestTheReadingNamesTheStoreThatTookNoWrite. A verdict on its own is what the crew already had, and
// it went to a container's log where nobody reading a console would find it.
func TestTheReadingNamesTheStoreThatTookNoWrite(t *testing.T) {
	server := waitingCrew(controlplane.Config{
		Store:  stalledStore{Store: store.NewMemory()},
		Events: messaging.NewMemory(),
	})
	reading := server.ProbeHealth(context.Background())

	state, detail := stateOf(reading, display.HealthStore)
	if state != display.HealthDown {
		t.Fatalf("a store that takes no write reads %q", state)
	}
	if !strings.Contains(detail, "the store did not take a write") {
		t.Fatalf("the reading says %q, and it does not say what did not land", detail)
	}
	// The log answered, and a reading that failed one part must not condemn the other.
	if state, _ := stateOf(reading, display.HealthEvents); state != display.HealthServing {
		t.Fatalf("the event log answered and reads %q", state)
	}
}

// TestTheReadingNamesTheEventLogThatNeverAnswered is issue 445 as a reading: the broker was gone for
// sixteen hours and every view of the crew was drawn from configuration, which had not changed.
func TestTheReadingNamesTheEventLogThatNeverAnswered(t *testing.T) {
	server := waitingCrew(controlplane.Config{Events: stalledLog{}})
	reading := server.ProbeHealth(context.Background())

	state, detail := stateOf(reading, display.HealthEvents)
	if state != display.HealthDown {
		t.Fatalf("an event log that never answers reads %q", state)
	}
	if !strings.Contains(detail, "the event log did not take a record") {
		t.Fatalf("the reading says %q, and it does not name the write that did not land", detail)
	}
	if state, _ := stateOf(reading, display.HealthStore); state != display.HealthServing {
		t.Fatalf("the store took the write and reads %q", state)
	}
}

// TestACrewWithNoEventLogSaysSoRatherThanReadingAsServing. A crew configured with none writes to a
// log that discards, so the write succeeds. Reading that as health is the failure this whole reading
// exists to stop, one step earlier: a crew recording nothing, drawn as a crew recording everything.
func TestACrewWithNoEventLogSaysSoRatherThanReadingAsServing(t *testing.T) {
	server := waitingCrew(controlplane.Config{})
	state, _ := stateOf(server.ProbeHealth(context.Background()), display.HealthEvents)
	if state != display.HealthNotConfigured {
		t.Fatalf("a crew with nothing connected to its log reads %q", state)
	}
}

// TestAskingHowTheCrewIsDoesNotProbeIt. The view that reads this draws on a timer, and the part that
// is down is the part that takes the export budget to say so. A call that probed would hand the
// operator a console that hangs for as long as the broker stays dead.
func TestAskingHowTheCrewIsDoesNotProbeIt(t *testing.T) {
	counting := &countingStore{Store: store.NewMemory()}
	server := waitingCrew(controlplane.Config{Store: counting, Events: messaging.NewMemory()})
	ctx := context.Background()

	server.ProbeHealth(ctx)
	after := counting.probes.Load()
	for range 3 {
		if _, err := server.GetHealth(ctx, &quaycrewv1.GetHealthRequest{}); err != nil {
			t.Fatalf("GetHealth: %v", err)
		}
	}
	if got := counting.probes.Load(); got != after {
		t.Fatalf("asking how the crew is probed the store %d more times", got-after)
	}
}

// TestACrewAnswersHowItIsWithWhatItLastFound, over the wire, so the console reads a probe rather than
// configuration.
func TestACrewAnswersHowItIsWithWhatItLastFound(t *testing.T) {
	server := waitingCrew(controlplane.Config{Events: stalledLog{}})
	ctx := context.Background()
	server.ProbeHealth(ctx)

	answer, err := server.GetHealth(ctx, &quaycrewv1.GetHealthRequest{})
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if answer.GetCheckedAt() == nil {
		t.Fatal("a crew that has probed does not say when")
	}
	states := map[string]string{}
	for _, component := range answer.GetComponents() {
		states[component.GetName()] = component.GetState()
	}
	if states[display.HealthEvents] != display.HealthDown {
		t.Fatalf("the crew says its dead event log is %q", states[display.HealthEvents])
	}
	if states[display.HealthStore] != display.HealthServing {
		t.Fatalf("the crew says its working store is %q", states[display.HealthStore])
	}
}

// TestACrewThatHasNeverProbedClaimsNothing. Answering serving before anything looked is the same lie
// the stats view told for sixteen hours, and a crew comes up before its first probe lands.
func TestACrewThatHasNeverProbedClaimsNothing(t *testing.T) {
	answer, err := waitingCrew(controlplane.Config{}).GetHealth(
		context.Background(), &quaycrewv1.GetHealthRequest{})
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if len(answer.GetComponents()) != 0 {
		t.Fatalf("a crew that has never probed says %d things about itself", len(answer.GetComponents()))
	}
	if answer.GetCheckedAt() != nil {
		t.Fatalf("a crew that has never probed says it was checked at %s", answer.GetCheckedAt().AsTime())
	}
}

package controlplane_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// stalledStore reads the way the real one does and never takes a write, which is the system this is
// all about: every listing answered and nothing started.
type stalledStore struct {
	store.Store
}

func (stalledStore) Probe(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestASystemThatCanWriteIsServing(t *testing.T) {
	health := controlplane.NewHealth(waitingSystem(controlplane.Config{}))
	answer, err := health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if answer.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("a system that writes answered %s", answer.GetStatus())
	}
}

// The check has to write, because the system it exists for answered every read in under a second while
// it started no work at all.
func TestASystemWhoseStoreTakesNoWriteIsNotServing(t *testing.T) {
	health := controlplane.NewHealth(waitingSystem(controlplane.Config{
		Store: stalledStore{Store: store.NewMemory()},
	}))
	answer, err := health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if answer.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("a system whose store takes no write answered %s", answer.GetStatus())
	}
}

// A system asked about its one service says the same thing about it.
func TestTheListingOfServicesSaysWhatTheCheckSays(t *testing.T) {
	health := controlplane.NewHealth(waitingSystem(controlplane.Config{}))
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

// TestTheReadingNamesTheStoreThatTookNoWrite. A verdict on its own is what the system already had, and
// it went to a container's log where nobody reading a console would find it.
func TestTheReadingNamesTheStoreThatTookNoWrite(t *testing.T) {
	server := waitingSystem(controlplane.Config{Store: stalledStore{Store: store.NewMemory()}})
	reading := server.ProbeHealth(context.Background())

	state, detail := stateOf(reading, display.HealthStore)
	if state != display.HealthDown {
		t.Fatalf("a store that takes no write reads %q", state)
	}
	if !strings.Contains(detail, "the store did not take a write") {
		t.Fatalf("the reading says %q, and it does not say what did not land", detail)
	}
}

// TestAskingHowTheSystemIsDoesNotProbeIt. The view that reads this draws on a timer, and a part that is
// down is the part that takes the whole budget to say so. A call that probed would hand the operator a
// console that hangs for as long as that part stays dead.
func TestAskingHowTheSystemIsDoesNotProbeIt(t *testing.T) {
	counting := &countingStore{Store: store.NewMemory()}
	server := waitingSystem(controlplane.Config{Store: counting})
	ctx := context.Background()

	server.ProbeHealth(ctx)
	after := counting.probes.Load()
	for range 3 {
		if _, err := server.GetHealth(ctx, &quaycrewv1.GetHealthRequest{}); err != nil {
			t.Fatalf("GetHealth: %v", err)
		}
	}
	if got := counting.probes.Load(); got != after {
		t.Fatalf("asking how the system is probed the store %d more times", got-after)
	}
}

// TestASystemAnswersHowItIsWithWhatItLastFound, over the wire, so the console reads a probe rather than
// configuration.
func TestASystemAnswersHowItIsWithWhatItLastFound(t *testing.T) {
	server := waitingSystem(controlplane.Config{Store: stalledStore{Store: store.NewMemory()}})
	ctx := context.Background()
	server.ProbeHealth(ctx)

	answer, err := server.GetHealth(ctx, &quaycrewv1.GetHealthRequest{})
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if answer.GetCheckedAt() == nil {
		t.Fatal("a system that has probed does not say when")
	}
	states := map[string]string{}
	for _, component := range answer.GetComponents() {
		states[component.GetName()] = component.GetState()
	}
	if states[display.HealthStore] != display.HealthDown {
		t.Fatalf("the system says its stalled store is %q", states[display.HealthStore])
	}
}

// TestASystemThatHasNeverProbedClaimsNothing. Answering serving before anything looked is the same lie
// the stats view told for sixteen hours, and a system comes up before its first probe lands.
func TestASystemThatHasNeverProbedClaimsNothing(t *testing.T) {
	answer, err := waitingSystem(controlplane.Config{}).GetHealth(
		context.Background(), &quaycrewv1.GetHealthRequest{})
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if len(answer.GetComponents()) != 0 {
		t.Fatalf("a system that has never probed says %d things about itself", len(answer.GetComponents()))
	}
	if answer.GetCheckedAt() != nil {
		t.Fatalf("a system that has never probed says it was checked at %s", answer.GetCheckedAt().AsTime())
	}
}

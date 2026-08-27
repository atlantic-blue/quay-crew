package controlplane_test

import (
	"context"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/controlplane"
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

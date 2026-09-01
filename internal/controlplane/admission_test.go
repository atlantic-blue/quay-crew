package controlplane_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/headroom"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// These drive the whole path a job takes through a real system: the controller decides, the ledger
// counts, the reading of the runtime says what there is, and the store holds what came of it. The
// runtime is the one thing stood in for, because a test cannot fill a machine on purpose.

// aRuntime is a container runtime of a given size, as the system reads one. A test changes what it
// says and asks the system to read it again, because a machine that fills up and empties again is the
// whole point and a scenario cannot fill a real one.
type aRuntime struct {
	mu        sync.Mutex
	memory    int64
	processor float64
	// held is what its containers are using, which is what the system's own reserve is measured from.
	held      int64
	heldShare float64
}

func (r *aRuntime) Sample(context.Context) (headroom.Sample, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return headroom.Sample{
		TakenAt:    time.Now(),
		Limit:      headroom.Measured(r.memory),
		Processors: headroom.MeasuredShare(r.processor),
		Used:       headroom.Measured(r.held),
		Held:       headroom.MeasuredShare(r.heldShare),
	}, nil
}

// frees is the system's own containers letting go of memory, which is what makes room for a sandbox.
func (r *aRuntime) frees(held int64, share float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.held, r.heldShare = held, share
}

func mebibytes(count int64) int64 { return count << 20 }

// aSystemOn is a system running on a runtime of that size, with the tick driven by the test.
func aSystemOn(t *testing.T, runtime *aRuntime, reserve capacity.Request) *controlplane.Server {
	t.Helper()
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "the bill is due on the 14th"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Headroom: runtime, HeadroomEvery: time.Hour, SystemReserve: reserve,
	})
	// One reading, taken now rather than on the sampler's own timer: a test that waited for a tick
	// would be a test with a clock in it.
	server.SampleHeadroom(context.Background())
	return server
}

// declareJob writes one job the way a caller does, and returns it.
func aJobIn(t *testing.T, s *controlplane.Server, workspace, project, title string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDepth: 3},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	made, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: title, Brief: "open the bill and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return made.GetJob().GetId()
}

func jobNow(t *testing.T, s *controlplane.Server, id string) *quaycrewv1.Job {
	t.Helper()
	got, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return got.GetJob()
}

// The acceptance test for the whole change: a job the runtime cannot host waits, and it is never
// admitted and then killed. The runtime here is the one that went down, 7,653 mebibytes and fourteen
// processors, with the system's own containers holding 6,000 of that memory.
func TestAJobIsHeldPendingOnAFullRuntimeRatherThanFailed(t *testing.T) {
	server := aSystemOn(t, &aRuntime{
		memory: mebibytes(7653), processor: 1400, held: mebibytes(6500), heldShare: 200,
	}, capacity.Request{Memory: mebibytes(2048), Processor: 200})
	ctx := context.Background()
	workspace, project := newProject(t, server)
	one := aJobIn(t, server, workspace, project, "read the electricity bill")

	server.TickJob(ctx)

	got := jobNow(t, server, one)
	if got.GetPhase() != job.PhasePending {
		t.Fatalf("the job is %q, want pending: a full machine has room again later", got.GetPhase())
	}
	if !strings.Contains(got.GetReason(), "not enough memory") {
		t.Fatalf("the job says %q, and it does not name the resource that ran out", got.GetReason())
	}
	if got.GetSession() != "" {
		t.Errorf("a job that was never admitted is in session %q", got.GetSession())
	}
}

// And the same job runs, untouched, once the runtime has room. Nothing had to be declared again,
// because nothing failed.
func TestTheHeldJobRunsWhenTheRuntimeHasRoomAgain(t *testing.T) {
	full := &aRuntime{memory: mebibytes(7653), processor: 1400, held: mebibytes(6500), heldShare: 200}
	server := aSystemOn(t, full, capacity.Request{Memory: mebibytes(2048), Processor: 200})
	ctx := context.Background()
	workspace, project := newProject(t, server)
	one := aJobIn(t, server, workspace, project, "read the electricity bill")

	server.TickJob(ctx)
	if jobNow(t, server, one).GetPhase() != job.PhasePending {
		t.Fatal("the job did not wait on a full runtime")
	}

	// The system's own containers let go of four gibibytes, and the system reads its machine again.
	full.frees(mebibytes(2000), 100)
	server.SampleHeadroom(ctx)
	server.TickJob(ctx)

	got := jobNow(t, server, one)
	if got.GetPhase() != job.PhaseRunning && got.GetPhase() != job.PhaseDone {
		t.Fatalf("the job is %q with room on the machine, want running", got.GetPhase())
	}
	if got.GetReason() != "" {
		t.Errorf("the job still says %q, and it is out of the wait that described", got.GetReason())
	}
}

// The burst. Nine jobs were declared against a runtime with room for fewer, and every one of them
// was admitted because a container appears seconds after the job that asked for it. The ledger is
// what makes the second job count the first, so the system admits what fits and holds the rest.
func TestABurstOfJobsIsAdmittedOnlyAsFarAsTheRuntimeGoes(t *testing.T) {
	// 7,653 mebibytes with 2,048 reserved leaves 5,605, which is three sandboxes at 1,536 and no
	// fourth. The processors are wide open, so memory is what binds.
	server := aSystemOn(t, &aRuntime{memory: mebibytes(7653), processor: 1400, held: 0, heldShare: 0},
		capacity.Request{Memory: mebibytes(2048), Processor: 200})
	ctx := context.Background()
	workspace, project := newProject(t, server)

	declared := make([]string, 0, 9)
	for range 9 {
		declared = append(declared, aJobIn(t, server, workspace, project, "read the electricity bill"))
	}

	server.TickJob(ctx)

	running, held := 0, 0
	for _, id := range declared {
		got := jobNow(t, server, id)
		switch {
		case got.GetPhase() == job.PhasePending && got.GetReason() != "":
			held++
		case got.GetPhase() == job.PhasePending:
			t.Fatalf("job %s is pending and says nothing about why", id)
		default:
			running++
		}
	}
	if running != 3 {
		t.Fatalf("%d of nine jobs were started on a runtime with room for three", running)
	}
	if held != 6 {
		t.Fatalf("%d jobs are waiting, want the other six", held)
	}
}

// The system's own containers are the reserve, and they are measured rather than declared: a kubelet
// runs outside the pods it manages and the control plane does not. A system whose own containers grow
// hands out less, without anybody changing a setting.
func TestTheSystemsOwnContainersAreReservedAsTheyGrow(t *testing.T) {
	// The floor is small, and the system's own containers are holding far more than it.
	server := aSystemOn(t, &aRuntime{memory: mebibytes(8192), processor: 1400, held: mebibytes(7000), heldShare: 100},
		capacity.Request{Memory: mebibytes(256), Processor: 100})
	ctx := context.Background()
	workspace, project := newProject(t, server)
	one := aJobIn(t, server, workspace, project, "read the electricity bill")

	server.TickJob(ctx)

	got := jobNow(t, server, one)
	if got.GetPhase() != job.PhasePending {
		t.Fatalf("the job is %q: the system handed out memory its own containers are holding",
			got.GetPhase())
	}
}

// A system that cannot read its runtime runs the work. There is no arithmetic to do for a system whose
// sessions do not run on a container runtime at all, and stopping dead would be worse than the system
// that counted.
func TestASystemWithNoRuntimeToReadStillRunsJobs(t *testing.T) {
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "done"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()
	workspace, project := newProject(t, server)
	one := aJobIn(t, server, workspace, project, "read the electricity bill")

	server.TickJob(ctx)

	if got := jobNow(t, server, one); got.GetPhase() == job.PhasePending {
		t.Fatalf("a system with no runtime to read held a job: %s", got.GetReason())
	}
}

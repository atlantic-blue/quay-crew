package job_test

import (
	"context"
	"sync"
	"testing"

	"github.com/atlantic-blue/krewe/internal/job"
)

// theCredentials records what the system was asked to take back, which is the whole of what a
// controller does about a credential: it never holds one.
type theCredentials struct {
	mu    sync.Mutex
	taken []string
}

func (c *theCredentials) RevokeJobCredentials(id, phase string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.taken = append(c.taken, id+" "+phase)
}

func (c *theCredentials) took() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.taken...)
}

// A job that ended has its credentials taken back, and the phase it ended in is what the refusal
// afterwards names. Every way a job can end, because a credential that outlived one of them would
// outlive the job in exactly the case nobody tests.
func TestACredentialIsTakenBackWheneverItsJobEnds(t *testing.T) {
	for _, one := range []struct {
		name string
		end  func(plane *system)
		want string
	}{
		{
			name: "the task answered",
			end:  func(plane *system) { plane.lands("the bill is due on the 14th") },
			want: "job-1 " + job.PhaseDone,
		},
		{
			name: "the task failed",
			end:  func(plane *system) { plane.fails("the sandbox went away") },
			want: "job-1 " + job.PhaseFailed,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			kept, plane := newRows(), newSystem()
			credentials := &theCredentials{}
			controller := job.NewController(kept, plane, nil, nil, nil).Revoking(credentials)
			kept.add(declaredJob("read the electricity bill"))
			ctx := context.Background()

			controller.Tick(ctx)
			if took := credentials.took(); len(took) != 0 {
				t.Fatalf("the system took %v back while the job was still running", took)
			}

			one.end(plane)
			controller.Tick(ctx)

			took := credentials.took()
			if len(took) != 1 || took[0] != one.want {
				t.Fatalf("the system took back %v, want [%q]", took, one.want)
			}
		})
	}
}

// A system with no revoker runs the job exactly the same. It is the system a test builds, and a
// controller that needed one to finish a job would be a controller that could not run without a
// control plane beside it.
func TestAControllerWithNoRevokerStillRunsTheJob(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q on a system that takes no credential back", got.Phase)
	}
}

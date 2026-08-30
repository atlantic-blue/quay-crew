package job_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/job"
)

// A job names a role, the session runs as that role, and what the job requires is held
// against what the role receives. This is the boundary the whole substrate was built for, so these
// tests are about the two directions it can fail in: a session that runs as nobody, and a session
// handed job its role was never meant to see.

// roles is a double for the roles a system holds: a name, what it receives, and whether it can be read
// at all. A system that cannot read its roles is its own case, because a check that quietly passes
// when it could not be run is the same false green as no check.
type roles struct {
	receives map[string][]string
	refuse   error
}

func rolesReceiving(name string, material ...string) *roles {
	return &roles{receives: map[string][]string{name: material}}
}

func (r *roles) RoleFor(_ context.Context, _, named string) (job.Receiver, error) {
	if r.refuse != nil {
		return nil, r.refuse
	}
	held, found := r.receives[named]
	if !found {
		return nil, fmt.Errorf("this workspace does not hold the %s role, so nothing can run as it", named)
	}
	return receivesOnly(held), nil
}

// receivesOnly is a role as the boundary sees one: it answers what it is given and nothing else.
type receivesOnly []string

func (r receivesOnly) Gets(material string) bool {
	for _, held := range r {
		if held == material {
			return true
		}
	}
	return false
}

// jobInRole is a job that names a role and requires some material of it.
func jobInRole(named string, required ...string) *job.Job {
	one := declaredJob("clear the open pull request backlog")
	one.Role, one.RoleVersion, one.Requires = named, 1, required
	return one
}

// The reason the substrate was built: the session that runs the job runs as the role the job
// names, so the credential the system mints for that task carries what the role declared it may call.
func TestJobThatNamesARoleRunsAsThatRole(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("backlog-clearer", "job"))
	one := kept.add(jobInRole("backlog-clearer"))

	controller.Tick(context.Background())

	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
	if got := plane.dispatched[0].GetRole(); got != "backlog-clearer" {
		t.Fatalf("the task runs as %q, want the role the job names", got)
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running", got.Phase, got.Reason)
	}
}

// The role comes off the row, never from the caller of the task. A caller that could name its own
// role could name one granting more than the job was declared with.
func TestTheRoleOnTheTaskIsTheRoleOnTheRow(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("test-writer", "job"))
	kept.add(jobInRole("test-writer"))

	controller.Tick(context.Background())

	if got := plane.dispatched[0].GetRole(); got != "test-writer" {
		t.Fatalf("the task runs as %q, want test-writer from the row", got)
	}
	if got := plane.dispatched[0].GetJob(); got == "" {
		t.Fatalf("the task names no job, so the system would mint no credential for it")
	}
}

// Job with no role is exactly what it was: no role on the task, and nothing about it changed.
func TestJobWithNoRoleIsDispatchedAsNobody(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("backlog-clearer", "job"))
	one := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if got := plane.dispatched[0].GetRole(); got != "" {
		t.Fatalf("the task runs as %q, want as nobody", got)
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running", got.Phase, got.Reason)
	}
}

// The boundary, in the direction that matters. The job needs the system's context and the role never
// receives it, so no container is ever built for it.
func TestJobRequiringMaterialItsRoleDoesNotReceiveIsStoppedBeforeAnyDispatch(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("test-writer", "job"))
	one := kept.add(jobInRole("test-writer", "context"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the system was asked to run %d tasks, want none: no container starts for refused job", plane.sent())
	}
	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	for _, want := range []string{"test-writer", "context", "declare the job without"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("the refusal says %q, want it to name %q", got.Reason, want)
		}
	}
	if got.Session != "" {
		t.Fatalf("the refused job ran in session %q, and no session should exist", got.Session)
	}
}

// The record has to read the way it happened. Job that was claimed and refused was never started,
// so nothing on its history may say it was.
func TestRefusedJobIsClaimedAndStoppedAndNeverStarted(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("test-writer", "job"))
	one := kept.add(jobInRole("test-writer", "skills"))

	controller.Tick(context.Background())

	kinds := kept.kinds(one.ID)
	if strings.Join(kinds, ",") != job.EventClaimed+","+job.EventStopped {
		t.Fatalf("the records read %v, want claimed then stopped", kinds)
	}
}

// The boundary holding is not the same as no boundary. Job requiring what its role does receive
// runs.
func TestJobRequiringWhatItsRoleReceivesRuns(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("backlog-clearer", "job", "context"))
	one := kept.add(jobInRole("backlog-clearer", "context", "job"))

	controller.Tick(context.Background())

	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running", got.Phase, got.Reason)
	}
}

// A role can be detached while a job sits pending, which is why the check is here and not only at the
// write. The job stops, naming the role, rather than running as nobody.
func TestJobNamingARoleTheSystemNoLongerHoldsIsStopped(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("someone-else", "job"))
	one := kept.add(jobInRole("backlog-clearer"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the system was asked to run %d tasks, want none", plane.sent())
	}
	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped || !strings.Contains(got.Reason, "backlog-clearer") {
		t.Fatalf("the job is %q saying %q, want stopped naming the role", got.Phase, got.Reason)
	}
}

// A read that failed is not a boundary that held. The job stops rather than running unchecked.
func TestJobInARoleTheSystemCannotReadIsStopped(t *testing.T) {
	kept, plane := newRows(), newSystem()
	held := rolesReceiving("backlog-clearer", "job")
	held.refuse = errors.New("the store went away")
	controller := job.NewController(kept, plane, nil, nil, nil).Reading(held)
	one := kept.add(jobInRole("backlog-clearer", "context"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the system was asked to run %d tasks, want none", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped: a check that could not be run is not a check that passed", got.Phase)
	}
}

// A controller wired with no way to read a role refuses job in one, for the same reason. Job with
// no role is untouched by any of it.
func TestAControllerThatCannotReadRolesStopsJobInARoleAndRunsTheRest(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil)
	inRole := kept.add(jobInRole("backlog-clearer"))
	plain := declaredJob("read the electricity bill")
	plain.ID = "job-2"
	kept.add(plain)

	controller.Tick(context.Background())

	if got := kept.get(inRole.ID); got.Phase != job.PhaseStopped ||
		!strings.Contains(got.Reason, "cannot read its roles") {
		t.Fatalf("the job in a role is %q saying %q, want stopped saying the roles cannot be read",
			got.Phase, got.Reason)
	}
	if got := kept.get("job-2"); got.Phase != job.PhaseRunning {
		t.Fatalf("the job with no role is %q, want running: a role nobody named changes nothing", got.Phase)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want the one with no role", plane.sent())
	}
}

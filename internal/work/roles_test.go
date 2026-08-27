package work_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/work"
)

// A piece of work names a role, the session runs as that role, and what the work hands is held
// against what the role receives. This is the boundary the whole substrate was built for, so these
// tests are about the two directions it can fail in: a session that runs as nobody, and a session
// handed work its role was never meant to see.

// roles is a double for the roles a crew holds: a name, what it receives, and whether it can be read
// at all. A crew that cannot read its roles is its own case, because a check that quietly passes
// when it could not be run is the same false green as no check.
type roles struct {
	receives map[string][]string
	refuse   error
}

func rolesReceiving(name string, material ...string) *roles {
	return &roles{receives: map[string][]string{name: material}}
}

func (r *roles) RoleFor(_ context.Context, _, named string) (work.Receiver, error) {
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

// workInRole is a piece of work that names a role and hands it some material.
func workInRole(named string, handed ...string) *work.Work {
	one := declaredWork("clear the open pull request backlog")
	one.Role, one.RoleVersion, one.Hands = named, 1, handed
	return one
}

// The reason the substrate was built: the session that runs the work runs as the role the work
// names, so the credential the crew mints for that task carries what the role declared it may call.
func TestWorkThatNamesARoleRunsAsThatRole(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("backlog-clearer", "work"))
	one := kept.add(workInRole("backlog-clearer"))

	controller.Tick(context.Background())

	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want 1", plane.sent())
	}
	if got := plane.dispatched[0].GetRole(); got != "backlog-clearer" {
		t.Fatalf("the task runs as %q, want the role the work names", got)
	}
	if got := kept.get(one.ID); got.Phase != work.PhaseRunning {
		t.Fatalf("the work is %q saying %q, want running", got.Phase, got.Reason)
	}
}

// The role comes off the row, never from the caller of the task. A caller that could name its own
// role could name one granting more than the work was declared with.
func TestTheRoleOnTheTaskIsTheRoleOnTheRow(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("test-writer", "work"))
	kept.add(workInRole("test-writer"))

	controller.Tick(context.Background())

	if got := plane.dispatched[0].GetRole(); got != "test-writer" {
		t.Fatalf("the task runs as %q, want test-writer from the row", got)
	}
	if got := plane.dispatched[0].GetWork(); got == "" {
		t.Fatalf("the task names no work, so the crew would mint no credential for it")
	}
}

// Work with no role is exactly what it was: no role on the task, and nothing about it changed.
func TestWorkWithNoRoleIsDispatchedAsNobody(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("backlog-clearer", "work"))
	one := kept.add(declaredWork("read the electricity bill"))

	controller.Tick(context.Background())

	if got := plane.dispatched[0].GetRole(); got != "" {
		t.Fatalf("the task runs as %q, want as nobody", got)
	}
	if got := kept.get(one.ID); got.Phase != work.PhaseRunning {
		t.Fatalf("the work is %q saying %q, want running", got.Phase, got.Reason)
	}
}

// The boundary, in the direction that matters. The work needs the crew's context and the role never
// receives it, so no container is ever built for it.
func TestWorkHandedMaterialItsRoleDoesNotReceiveIsStoppedBeforeAnyDispatch(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("test-writer", "work"))
	one := kept.add(workInRole("test-writer", "context"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the crew was asked to run %d tasks, want none: no container starts for refused work", plane.sent())
	}
	got := kept.get(one.ID)
	if got.Phase != work.PhaseStopped {
		t.Fatalf("the work is %q, want stopped", got.Phase)
	}
	for _, want := range []string{"test-writer", "context", "declare the work without"} {
		if !strings.Contains(got.Reason, want) {
			t.Fatalf("the refusal says %q, want it to name %q", got.Reason, want)
		}
	}
	if got.Session != "" {
		t.Fatalf("the refused work ran in session %q, and no session should exist", got.Session)
	}
}

// The record has to read the way it happened. Work that was claimed and refused was never started,
// so nothing on its history may say it was.
func TestRefusedWorkIsClaimedAndStoppedAndNeverStarted(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("test-writer", "work"))
	one := kept.add(workInRole("test-writer", "skills"))

	controller.Tick(context.Background())

	kinds := kept.kinds(one.ID)
	if strings.Join(kinds, ",") != work.EventClaimed+","+work.EventStopped {
		t.Fatalf("the records read %v, want claimed then stopped", kinds)
	}
}

// The boundary holding is not the same as no boundary. Work handed what its role does receive runs.
func TestWorkHandedWhatItsRoleReceivesRuns(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("backlog-clearer", "work", "context"))
	one := kept.add(workInRole("backlog-clearer", "context", "work"))

	controller.Tick(context.Background())

	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want 1", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != work.PhaseRunning {
		t.Fatalf("the work is %q saying %q, want running", got.Phase, got.Reason)
	}
}

// A role can be detached while work sits pending, which is why the check is here and not only at the
// write. The work stops, naming the role, rather than running as nobody.
func TestWorkNamingARoleTheCrewNoLongerHoldsIsStopped(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil).
		Reading(rolesReceiving("someone-else", "work"))
	one := kept.add(workInRole("backlog-clearer"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the crew was asked to run %d tasks, want none", plane.sent())
	}
	got := kept.get(one.ID)
	if got.Phase != work.PhaseStopped || !strings.Contains(got.Reason, "backlog-clearer") {
		t.Fatalf("the work is %q saying %q, want stopped naming the role", got.Phase, got.Reason)
	}
}

// A read that failed is not a boundary that held. The work stops rather than running unchecked.
func TestWorkInARoleTheCrewCannotReadIsStopped(t *testing.T) {
	kept, plane := newRows(), newCrew()
	held := rolesReceiving("backlog-clearer", "work")
	held.refuse = errors.New("the store went away")
	controller := work.NewController(kept, plane, nil, nil, nil).Reading(held)
	one := kept.add(workInRole("backlog-clearer", "context"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the crew was asked to run %d tasks, want none", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != work.PhaseStopped {
		t.Fatalf("the work is %q, want stopped: a check that could not be run is not a check that passed", got.Phase)
	}
}

// A controller wired with no way to read a role refuses work in one, for the same reason. Work with
// no role is untouched by any of it.
func TestAControllerThatCannotReadRolesStopsWorkInARoleAndRunsTheRest(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, nil, nil)
	inRole := kept.add(workInRole("backlog-clearer"))
	plain := declaredWork("read the electricity bill")
	plain.ID = "work-2"
	kept.add(plain)

	controller.Tick(context.Background())

	if got := kept.get(inRole.ID); got.Phase != work.PhaseStopped ||
		!strings.Contains(got.Reason, "cannot read its roles") {
		t.Fatalf("the work in a role is %q saying %q, want stopped saying the roles cannot be read",
			got.Phase, got.Reason)
	}
	if got := kept.get("work-2"); got.Phase != work.PhaseRunning {
		t.Fatalf("the work with no role is %q, want running: a role nobody named changes nothing", got.Phase)
	}
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want the one with no role", plane.sent())
	}
}

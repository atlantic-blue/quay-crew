package job_test

import (
	"context"
	"errors"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The fourth query, and what the controller does with it.
//
// Every test here is about one thing before it is about anything else: the two times ship unset, and
// unset means nothing moves. The rest of the file only exists to prove the mechanism works once an
// operator writes a number, because a mechanism nobody can turn on is not shipped either.

// eyes is an attachment double: it says who is in a session, and can refuse to answer at all, which
// is the case a system has to read as attached rather than as nobody.
type eyes struct {
	watching map[string]bool
	refuse   error
	asked    []string
}

func (e *eyes) SessionAttached(_ context.Context, session string) (bool, error) {
	e.asked = append(e.asked, session)
	if e.refuse != nil {
		return false, e.refuse
	}
	return e.watching[session], nil
}

// aSettledSession is a live session waiting for work, last touched however long ago.
func aSettledSession(id string, idleFor time.Duration) *quaycrewv1.Session {
	return &quaycrewv1.Session{
		Id: id, Workspace: "workspace-1", Project: "project-1", Handle: id, Status: "idle",
		UpdatedAt: timestamppb.New(time.Now().UTC().Add(-idleFor)),
	}
}

// aReclaimedSession is one the system already took the container back from, that long ago.
func aReclaimedSession(id string, reclaimedFor time.Duration) *quaycrewv1.Session {
	at := timestamppb.New(time.Now().UTC().Add(-reclaimedFor))
	return &quaycrewv1.Session{
		Id: id, Workspace: "workspace-1", Project: "project-1", Handle: id, Status: "reclaimed",
		UpdatedAt: at, ReclaimedAt: at,
	}
}

// aLifecycleController is a controller that can see sessions and can tell who is in them.
func aLifecycleController(t *testing.T) (*job.Controller, *rows, *system, *eyes) {
	t.Helper()
	kept, plane := newRows(), newSystem()
	plane.store = kept
	watching := &eyes{watching: map[string]bool{}}
	return job.NewController(kept, plane, nil, nil, nil).Watching(watching), kept, plane, watching
}

// The rule that is not negotiable. Both times ship unset, and unset changes nothing, however long the
// loop runs and however long a session has been sitting there.
func TestWithBothTimesUnsetNothingIsReclaimedAndNothingIsArchived(t *testing.T) {
	controller, kept, plane, watching := aLifecycleController(t)
	kept.addSession(aSettledSession("session-idle-for-a-year", 365*24*time.Hour))
	kept.addSession(aReclaimedSession("session-reclaimed-for-a-year", 365*24*time.Hour))
	ctx := context.Background()

	// Twenty ticks rather than one, because "however long it runs" is the claim being made.
	for range 20 {
		controller.Tick(ctx)
	}

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the controller reclaimed %v, and no workspace gave it a reclaim time", got)
	}
	if got := plane.archived(); len(got) != 0 {
		t.Fatalf("the controller archived %v, and no workspace gave it an archive time", got)
	}
	if kept.sessionStatus("session-idle-for-a-year") != "idle" {
		t.Fatalf("the idle session reads %q, want it untouched", kept.sessionStatus("session-idle-for-a-year"))
	}
	// Nothing was even asked, which is the cost claim: a system with no times set pays no exec per
	// session per tick for a signal it will not act on.
	if len(watching.asked) != 0 {
		t.Fatalf("the system asked whether somebody was in %v, and it had no reason to look", watching.asked)
	}
}

// The mechanism, once an operator writes a number.
func TestASessionIdleLongerThanTheReclaimTimeGivesItsContainerBack(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	kept.addSession(aSettledSession("session-quiet", 2*time.Minute))
	ctx := context.Background()

	controller.Tick(ctx)

	if got := plane.reclaimed(); len(got) != 1 || got[0] != "session-quiet" {
		t.Fatalf("the controller reclaimed %v, want the one session that was past its time", got)
	}
	if status := kept.sessionStatus("session-quiet"); status != "reclaimed" {
		t.Fatalf("the session reads %q, want reclaimed", status)
	}
}

// The other half of the number: a session that has not been quiet for long enough keeps its
// container, so a reclaim time is a threshold rather than a switch.
func TestASessionInsideTheReclaimTimeKeepsItsContainer(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 3600})
	kept.addSession(aSettledSession("session-recent", time.Minute))
	ctx := context.Background()

	controller.Tick(ctx)

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the controller reclaimed %v, and it had been quiet for a minute against an hour", got)
	}
}

// The dangerous one. A container an operator is typing into is never taken, whatever the clock says.
func TestASessionSomebodyIsInIsNeverReclaimed(t *testing.T) {
	controller, kept, plane, watching := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	kept.addSession(aSettledSession("session-open", 365*24*time.Hour))
	watching.watching["session-open"] = true
	ctx := context.Background()

	for range 5 {
		controller.Tick(ctx)
	}

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the controller reclaimed %v while somebody had it open", got)
	}
	if kept.sessionStatus("session-open") != "idle" {
		t.Fatalf("the session reads %q, want it left exactly as it was", kept.sessionStatus("session-open"))
	}
}

// A system that cannot tell must read that as attached. The two mistakes are not the same size.
func TestASessionTheSystemCannotSeeIntoIsNeverReclaimed(t *testing.T) {
	controller, kept, plane, watching := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	kept.addSession(aSettledSession("session-unreadable", 365*24*time.Hour))
	watching.refuse = errors.New("the daemon did not answer")
	ctx := context.Background()

	controller.Tick(ctx)

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the controller reclaimed %v after failing to find out whether anybody was in it", got)
	}
}

// A controller nobody wired a signal into reclaims nothing, so turning the mechanism on is wiring
// rather than a number alone.
func TestAControllerWithNoWayToLookReclaimsNothing(t *testing.T) {
	kept, plane := newRows(), newSystem()
	plane.store = kept
	controller := job.NewController(kept, plane, nil, nil, nil)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	kept.addSession(aSettledSession("session-quiet", 365*24*time.Hour))

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("a controller that cannot ask who is in a session reclaimed %v", got)
	}
}

// Job still open holds its session alive, which is the whole reason the lifecycle is derived from
// the job rather than declared on the session.
func TestASessionJobStillNamesIsNeverReclaimed(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	kept.addSession(aSettledSession("session-busy", 365*24*time.Hour))
	open := declaredJob("read the electricity bill")
	open.Phase, open.Session = job.PhaseRunning, "session-busy"
	kept.add(open)

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 0 {
		t.Fatalf("the controller reclaimed %v while a job still named it", got)
	}
}

// Job that ended stops holding its session, so the same session becomes a candidate.
func TestASessionOnlyEndedJobNamesIsReclaimed(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	kept.addSession(aSettledSession("session-finished", 365*24*time.Hour))
	done := declaredJob("read the electricity bill")
	done.Phase, done.Session = job.PhaseDone, "session-finished"
	kept.add(done)

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 1 || got[0] != "session-finished" {
		t.Fatalf("the controller reclaimed %v, want the session whose job had ended", got)
	}
}

// The second step, and it is measured against the reclaim stamp rather than against the row's last
// write, so an unrelated update cannot hold a session out of the archive forever.
func TestASessionReclaimedLongerThanTheArchiveTimeIsFiledAway(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60, ArchiveSeconds: 120})
	kept.addSession(aReclaimedSession("session-old", 10*time.Minute))

	controller.Tick(context.Background())

	if got := plane.archived(); len(got) != 1 || got[0] != "session-old" {
		t.Fatalf("the controller archived %v, want the session that had been reclaimed longest", got)
	}
}

func TestAReclaimedSessionInsideTheArchiveTimeIsLeftAlone(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60, ArchiveSeconds: 3600})
	kept.addSession(aReclaimedSession("session-recent", time.Minute))

	controller.Tick(context.Background())

	if got := plane.archived(); len(got) != 0 {
		t.Fatalf("the controller archived %v a minute into an hour", got)
	}
}

// A reclaim time on its own reclaims and never files away, so the two numbers are two decisions.
func TestAReclaimTimeWithNoArchiveTimeFilesNothingAway(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	kept.addSession(aReclaimedSession("session-old", 365*24*time.Hour))

	for range 10 {
		controller.Tick(context.Background())
	}

	if got := plane.archived(); len(got) != 0 {
		t.Fatalf("the controller archived %v with no archive time set", got)
	}
}

// One movement per session per tick, so every step is on the record separately rather than a session
// disappearing from idle to archived in one pass.
func TestASessionIsReclaimedOnOneTickAndArchivedOnALater(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60, ArchiveSeconds: 1})
	kept.addSession(aSettledSession("session-quiet", time.Hour))
	ctx := context.Background()

	controller.Tick(ctx)
	if got := plane.archived(); len(got) != 0 {
		t.Fatalf("the same tick that reclaimed it also archived %v", got)
	}
	// A second passes for the archive time, the way a later tick would find it.
	kept.reclaimedAgo("session-quiet", 5*time.Second)
	controller.Tick(ctx)

	if got := plane.archived(); len(got) != 1 || got[0] != "session-quiet" {
		t.Fatalf("the later tick archived %v, want the session it had reclaimed", got)
	}
}

// One workspace's number is not another's, because a ceiling is tenancy.
func TestOneWorkspacesReclaimTimeDoesNotReachAnother(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	kept.addSession(aSettledSession("session-here", time.Hour))
	elsewhere := aSettledSession("session-elsewhere", time.Hour)
	elsewhere.Workspace = "workspace-2"
	kept.addSession(elsewhere)

	controller.Tick(context.Background())

	if got := plane.reclaimed(); len(got) != 1 || got[0] != "session-here" {
		t.Fatalf("the controller reclaimed %v, want only the workspace that named a time", got)
	}
}

// A refusal from the system is a session that moved between the query and the write. It is logged and
// the tick carries on, because one row that would not move must not stop the others.
func TestASessionThatRefusesToBeReclaimedDoesNotStopTheOthers(t *testing.T) {
	controller, kept, plane, _ := aLifecycleController(t)
	kept.allow(job.Limits{Workspace: "workspace-1", ReclaimSeconds: 60})
	first := aSettledSession("session-a", 2*time.Hour)
	kept.addSession(first)
	kept.addSession(aSettledSession("session-b", time.Hour))
	plane.refuseReclaim = errors.New("a task arrived a moment ago")

	controller.Tick(context.Background())

	if status := kept.sessionStatus("session-b"); status != "reclaimed" {
		t.Fatalf("the second session reads %q after the first refused, want reclaimed", status)
	}
}

package controlplane_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A job that failed is continued rather than declared again, driven through the control plane's own
// interface: the session records what it finished, the task dies, the operator continues it, and the
// next task lands in the same conversation carrying the steps.
//
// The assertions go past the row. What decides whether this works is what the session is handed
// next, so these read the task record rather than stopping at the phase.

// aJobThatFailed is a job whose session recorded two steps and whose task then died for a reason
// that had nothing to do with the work, which is the shape this behaviour exists for.
func aJobThatFailed(t *testing.T, failure string) heldOpen {
	t.Helper()
	runner := &model.FakeRunner{
		Err: errors.New(failure), Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	kept := store.NewMemory()
	server := controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: runner, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	declared := declaredIn(t, server, "sort the listing")
	system := heldOpen{server: server, runner: runner, kept: kept, job: declared}
	ctx := context.Background()
	server.TickJob(ctx)
	<-runner.Started

	for _, said := range []string{"read the issue", "cut the worktree from origin/main"} {
		if _, err := server.RecordJobStep(asJobCredential(ctx, declared.GetId()),
			&quaycrewv1.RecordJobStepRequest{Summary: said}); err != nil {
			t.Fatalf("RecordJobStep(%q): %v", said, err)
		}
	}

	// The task dies, and the controller writes that onto the row on its next pass. Waited for on the
	// task record rather than on the count: a task row exists the moment the dispatch lets go, and what
	// the controller reads is how that task ended.
	close(runner.Gate)
	waitFor(t, func() bool {
		sent := tasksOf(t, server, declared.GetId())
		return len(sent) == 1 && sent[0].GetStatus() == job.StatusFailed
	})
	server.TickJob(ctx)
	if phase := system.reading(t).GetPhase(); phase != job.PhaseFailed {
		t.Fatalf("the job is %q after its task died, want failed", phase)
	}
	return system
}

// The whole of it, end to end. Stopping at "the job is pending again" would prove half a feature:
// what decides whether anything was saved is what the second task carries.
func TestAJobThatFailedCarriesOnFromTheStepsItFinished(t *testing.T) {
	system := aJobThatFailed(t, "the credential ran out")
	ctx := context.Background()
	// The model works this time, so the continued attempt can land.
	system.runner.Err = nil
	system.runner.Gate, system.runner.Started = nil, nil

	resumed, err := system.server.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{Id: system.job.GetId()})
	if err != nil {
		t.Fatalf("ResumeJob: %v", err)
	}
	if resumed.GetJob().GetPhase() != job.PhasePending {
		t.Fatalf("a job being continued is %q, want pending so a controller starts it again",
			resumed.GetJob().GetPhase())
	}
	if resumed.GetJob().GetResuming() != "the credential ran out" {
		t.Fatalf("the job says it is continuing past %q, want what it failed with", resumed.GetJob().GetResuming())
	}

	system.server.TickJob(ctx)
	var sent []*quaycrewv1.Task
	waitFor(t, func() bool {
		sent = tasksOf(t, system.server, system.job.GetId())
		return len(sent) == 2
	})
	carried := sent[1].GetPrompt()
	for _, want := range []string{
		"read the issue", "cut the worktree from origin/main", "Do not do them again",
		"the credential ran out", "fetch the branch this work is based on",
	} {
		if !strings.Contains(carried, want) {
			t.Fatalf("the second task does not say %q:\n%s", want, carried)
		}
	}
	// The brief is not sent again. Sending it asks for the whole job a second time, which is the bill
	// this exists to stop paying.
	if strings.Contains(carried, system.job.GetBrief()) {
		t.Fatalf("the second task sends the brief again:\n%s", carried)
	}
	// And it is the same conversation, so the worktree, the branch and the pull request are the ones
	// the first attempt left behind.
	if sent[0].GetSession() != sent[1].GetSession() {
		t.Fatalf("the second task ran in session %s, and the first ran in %s",
			sent[1].GetSession(), sent[0].GetSession())
	}
}

// Continuing twice must not put two tasks into one conversation. The second call is refused on the
// row rather than on anything a caller remembers.
func TestAJobAlreadyGoingAgainIsNotContinuedASecondTime(t *testing.T) {
	system := aJobThatFailed(t, "the sandbox went away")
	ctx := context.Background()

	if _, err := system.server.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{Id: system.job.GetId()}); err != nil {
		t.Fatalf("ResumeJob: %v", err)
	}
	_, err := system.server.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{Id: system.job.GetId()})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a second resume answered %v, want it refused", err)
	}

	system.server.TickJob(ctx)
	waitFor(t, func() bool { return len(tasksOf(t, system.server, system.job.GetId())) == 2 })
	if sent := tasksOf(t, system.server, system.job.GetId()); len(sent) != 2 {
		t.Fatalf("%d tasks ran for this job, want the first and one more", len(sent))
	}
}

// The case that protects the operator. A failure that was the work being wrong is refused, and after
// that nothing continues it.
func TestAJobThatFailedBecauseTheWorkWasWrongIsRefusedAndThenNotContinued(t *testing.T) {
	system := aJobThatFailed(t, "the model did not finish")
	ctx := context.Background()

	refused, err := system.server.RefuseJob(ctx, &quaycrewv1.RefuseJobRequest{
		Id: system.job.GetId(), Reason: "the migration was wrong, this needs declaring again",
	})
	if err != nil {
		t.Fatalf("RefuseJob: %v", err)
	}
	if refused.GetJob().GetPhase() != job.PhaseStopped {
		t.Fatalf("a refused job is %q, want stopped", refused.GetJob().GetPhase())
	}
	// Both halves are on the row: what the operator decided, and the failure they were deciding about.
	for _, want := range []string{"the migration was wrong", "It failed with: the model did not finish"} {
		if !strings.Contains(refused.GetJob().GetReason(), want) {
			t.Fatalf("a refused job says %q, want it to carry %q", refused.GetJob().GetReason(), want)
		}
	}

	_, err = system.server.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{Id: system.job.GetId()})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a refused job was continued: %v", err)
	}
	if !strings.Contains(status.Convert(err).Message(), "on purpose") {
		t.Fatalf("the refusal says %q, want it to say the job was ended on purpose", err)
	}
	system.server.TickJob(ctx)
	if sent := tasksOf(t, system.server, system.job.GetId()); len(sent) != 1 {
		t.Fatalf("%d tasks ran for a refused job, want only the one that failed", len(sent))
	}
}

// A job that has not failed is neither continued nor refused. Continuing a running job would put a
// second task into a conversation that is working, and refusing a done one would erase how it ended.
func TestOnlyAJobThatFailedIsContinuedOrRefused(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	if _, err := system.server.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{
		Id: system.job.GetId(),
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a running job was continued: %v", err)
	}
	if _, err := system.server.RefuseJob(ctx, &quaycrewv1.RefuseJobRequest{
		Id: system.job.GetId(), Reason: "no",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a running job was refused: %v", err)
	}
	if phase := system.reading(t).GetPhase(); phase != job.PhaseRunning {
		t.Fatalf("the refused calls moved the job to %q", phase)
	}
}

// A session records against the job it is doing and no other. The identifier in the request is
// checked against the credential rather than trusted, because a caller that could name any job could
// write on any job's record.
func TestASessionCannotRecordAStepOnSomebodyElsesJob(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := context.Background()

	_, err := system.server.RecordJobStep(asJobCredential(ctx, system.job.GetId()),
		&quaycrewv1.RecordJobStepRequest{Summary: "read the issue", Id: "0123456789abcdef01234567"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("naming another job was accepted: %v", err)
	}
}

// A person is not doing a job, so what they finished is not a step of one.
func TestACallerRunningNoJobCannotRecordAStep(t *testing.T) {
	system := aJobUnderWay(t)

	_, err := system.server.RecordJobStep(context.Background(),
		&quaycrewv1.RecordJobStepRequest{Summary: "read the issue"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a caller doing no job recorded a step: %v", err)
	}
}

// A step with no words is refused rather than written. A record nobody can read tells the next
// attempt nothing, and it still costs it a line in front of the model.
func TestAStepWithNoWordsIsRefused(t *testing.T) {
	system := aJobUnderWay(t)

	_, err := system.server.RecordJobStep(asJobCredential(context.Background(), system.job.GetId()),
		&quaycrewv1.RecordJobStepRequest{Summary: "   "})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a step with no words was accepted: %v", err)
	}
}

// The record is the set of what is finished. A session that is continued says again what it said
// before, and the earlier steps must not be pushed down a list by it.
func TestTheSameStepTwiceLeavesOneStep(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := asJobCredential(context.Background(), system.job.GetId())

	for range 2 {
		if _, err := system.server.RecordJobStep(ctx,
			&quaycrewv1.RecordJobStepRequest{Summary: "read the issue"}); err != nil {
			t.Fatalf("RecordJobStep: %v", err)
		}
	}
	if steps := system.reading(t).GetSteps(); len(steps) != 1 {
		t.Fatalf("the job records %d steps, want one", len(steps))
	}
}

// The two answers to a failure are on the record, so somebody reading the run next week can see
// which one it got without opening a container that is long gone.
func TestContinuingAJobAndRefusingOneAreBothOnTheRecord(t *testing.T) {
	for _, answer := range []struct {
		name string
		give func(t *testing.T, system heldOpen)
		kind string
	}{
		{
			name: "continued",
			give: func(t *testing.T, system heldOpen) {
				if _, err := system.server.ResumeJob(context.Background(),
					&quaycrewv1.ResumeJobRequest{Id: system.job.GetId()}); err != nil {
					t.Fatalf("ResumeJob: %v", err)
				}
			},
			kind: job.EventResumed,
		},
		{
			name: "refused",
			give: func(t *testing.T, system heldOpen) {
				if _, err := system.server.RefuseJob(context.Background(), &quaycrewv1.RefuseJobRequest{
					Id: system.job.GetId(), Reason: "the migration was wrong",
				}); err != nil {
					t.Fatalf("RefuseJob: %v", err)
				}
			},
			kind: job.EventRefused,
		},
	} {
		t.Run(answer.name, func(t *testing.T) {
			system := aJobThatFailed(t, "the sandbox went away")
			answer.give(t, system)

			listed, err := system.kept.ListJobEvents(context.Background(), system.job.GetId())
			if err != nil {
				t.Fatalf("ListJobEvents: %v", err)
			}
			kinds := kindsOf(listed)
			if kinds[len(kinds)-1] != answer.kind {
				t.Fatalf("the records read %v, want the last to be %s", kinds, answer.kind)
			}
			var stepped int
			for _, one := range listed {
				if one.Kind == job.EventStepped {
					stepped++
				}
			}
			if stepped != 2 {
				t.Fatalf("the records read %v, want two saying a step was finished", kinds)
			}
		})
	}
}

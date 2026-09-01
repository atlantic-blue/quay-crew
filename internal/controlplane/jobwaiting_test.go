package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A job cannot wait, so the system refuses a brief that asks it to.
//
// The brief is the one the acceptance run was given. It pushed, it opened
// https://github.com/atlantic-blue/quay-crew/pull/462, and then it said it would hold until the
// checks landed. Nothing wakes a job, so it held forever and reported done in the same breath.

// The brief that started this, refused at the write with the graph named.
func TestABriefThatAsksTheJobToWaitIsRefusedNamingTheFlow(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "land the defect fix",
		Brief:      "fix the defect, push, watch the checks and merge on green",
		Repository: "atlantic-blue/quay-crew",
	})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("the refusal is %v, want InvalidArgument", status.Code(err))
	}
	for _, want := range []string{"watch the checks", "cannot wait", "krewe flow import"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal says %q, want it to say %q", err, want)
		}
	}
}

// A refused brief leaves nothing behind, so a listing does not carry a job nobody can run.
func TestABriefThatAsksTheJobToWaitLeavesNoRow(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	_ = refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "land the defect fix",
		Brief: "push the branch and merge the pull request once continuous integration is green",
	})

	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(listed.GetJobs()) != 0 {
		t.Fatalf("a refused declaration left %d rows behind", len(listed.GetJobs()))
	}
}

// The path this actually arrives on. An orchestrator session writes the brief for the child that
// ships, so the refusal has to reach a session declaring a job and not only an operator typing one.
func TestASessionIsRefusedTheSameBrief(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	workspace, project := newProject(t, s)
	raiseDepth(t, s, workspace, 2)
	parent := declareJob(t, s, project, "ship the defect fix")

	_, err := s.CreateJob(asJobCredential(context.Background(), parent.GetId(), role.VerbJobCreate),
		&quaycrewv1.CreateJobRequest{
			Project: project, Title: "release it",
			Brief: "open the pull request, then wait for the checks and merge on green",
		})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a session declaring it got %v, want InvalidArgument", status.Code(err))
	}
	if !strings.Contains(err.Error(), "cannot wait") {
		t.Errorf("the refusal says %q, want it to say a job cannot wait", err)
	}
}

// What the rule must not touch. Each of these is a brief somebody writes for this repository, and a
// refusal that fires on one of them is the rule everybody learns to word around.
func TestOrdinaryBriefsAreStillDeclared(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	for _, brief := range []string{
		"merge origin/main into the branch, then run the gates and push",
		"open the pull request and do not merge it",
		"push the branch and open the pull request; the merge is somebody else's",
		"run the test suite and wait for it to finish before reporting",
		"resolve the merge conflict in internal/store and push",
		"push the branch, then do not merge the pull request",
		"never merge the pull request yourself",
	} {
		created, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
			Project: project, Title: "land the defect fix", Brief: brief,
			Repository: "atlantic-blue/quay-crew", Mode: "dangerous",
		})
		if err != nil {
			t.Errorf("%q was refused: %v", brief, err)
			continue
		}
		if created.GetJob().GetBrief() != brief {
			t.Errorf("the job's brief reads back as %q", created.GetJob().GetBrief())
		}
	}
}

// A step of a flow is not a job somebody wrote, so it is not held to this rule.
//
// The graph around a step holds the wait. Its last node says "merge the pull request" and means it,
// and refusing that would refuse the very graph the refusal tells a caller to write. The engine
// declares a step through PrepareJob, which is the call this drives.
func TestAFlowStepThatMergesThePullRequestIsPrepared(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	carrier := declareJob(t, s, project, "flow ship version 1")

	step, _, err := s.PrepareJob(context.Background(), carrier.GetId(), job.Declaration{
		Project: project, Title: "ship step merge",
		Brief: "Merge the pull request. Every check passed.",
	})
	if err != nil {
		t.Fatalf("a flow's merge step was refused: %v", err)
	}
	if step.Parent != carrier.GetId() {
		t.Fatalf("the step hangs under %q, want the carrier", step.Parent)
	}
}

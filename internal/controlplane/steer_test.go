package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A steer is the score of a job. What is proved here is that the mark lands, that the count reaches
// the job at the top however deep the steer was made, and that a session cannot write its own score.

func TestASteerIsCountedOnTheJobItLandedOn(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "build the transcripts page")

	recorded, err := s.RecordSteer(context.Background(), &quaycrewv1.RecordSteerRequest{
		Job: declared.GetId(), Text: "the workspace has no secrets",
	})
	if err != nil {
		t.Fatalf("RecordSteer: %v", err)
	}
	if recorded.GetSteer().GetText() != "the workspace has no secrets" {
		t.Fatalf("the steer reads back as %q", recorded.GetSteer().GetText())
	}
	if recorded.GetSteer().GetOccurredAt() == nil {
		t.Fatal("the steer carries no moment, so a report could not put it in order")
	}
	if recorded.GetJob().GetSteers() != 1 {
		t.Fatalf("the job counts %d steers, want 1", recorded.GetJob().GetSteers())
	}

	read, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: declared.GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if read.GetJob().GetSteers() != 1 {
		t.Fatalf("reading the job back counts %d steers, want 1", read.GetJob().GetSteers())
	}
}

// The count belongs to the job the mark landed on, because that is the thing being scored. The job
// whose session declared it is a job in its own right, so a mark against one is not a mark against
// the other.
func TestASteerCountsOnTheJobItLandedOnAndOnNoOther(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	asked := declareJob(t, s, project, "build the transcripts page")
	caused := causedBy(t, s, asked, "fetch the captions")

	if _, err := s.RecordSteer(context.Background(), &quaycrewv1.RecordSteerRequest{
		Job: caused.GetId(), Text: "it chose a store that bills while idle",
	}); err != nil {
		t.Fatalf("RecordSteer: %v", err)
	}

	for _, counted := range []struct {
		name string
		id   string
		want int32
	}{
		{name: "the job it landed on", id: caused.GetId(), want: 1},
		{name: "the job that caused it", id: asked.GetId(), want: 0},
	} {
		read, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: counted.id})
		if err != nil {
			t.Fatalf("GetJob %s: %v", counted.name, err)
		}
		if read.GetJob().GetSteers() != counted.want {
			t.Fatalf("%s counts %d steers, want %d", counted.name, read.GetJob().GetSteers(), counted.want)
		}
	}
}

// The report is one job's marks, in the order they were made.
func TestTheReportReadsOneJobsMarksInOrder(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	asked := declareJob(t, s, project, "build the transcripts page")
	caused := causedBy(t, s, asked, "fetch the captions")

	for _, said := range []struct {
		on   string
		text string
	}{
		{on: caused.GetId(), text: "first"},
		{on: caused.GetId(), text: "second"},
	} {
		if _, err := s.RecordSteer(context.Background(), &quaycrewv1.RecordSteerRequest{
			Job: said.on, Text: said.text,
		}); err != nil {
			t.Fatalf("RecordSteer: %v", err)
		}
	}

	listed, err := s.ListSteers(context.Background(), &quaycrewv1.ListSteersRequest{Job: caused.GetId()})
	if err != nil {
		t.Fatalf("ListSteers: %v", err)
	}
	if len(listed.GetSteers()) != 2 {
		t.Fatalf("the report carries %d steers, want the job's 2", len(listed.GetSteers()))
	}
	if listed.GetSteers()[0].GetText() != "first" || listed.GetSteers()[1].GetText() != "second" {
		t.Fatalf("the report is out of order: %q then %q",
			listed.GetSteers()[0].GetText(), listed.GetSteers()[1].GetText())
	}
	if listed.GetJob().GetId() != caused.GetId() || listed.GetJob().GetSteers() != 2 {
		t.Fatalf("the report names %q with %d steers, want the job it was read against with 2",
			listed.GetJob().GetId(), listed.GetJob().GetSteers())
	}
	// And the job that caused it carries none of them: a steer belongs to one job.
	beside, err := s.ListSteers(context.Background(), &quaycrewv1.ListSteersRequest{Job: asked.GetId()})
	if err != nil || len(beside.GetSteers()) != 0 {
		t.Fatalf("the job that caused it reads back %d steers, want none", len(beside.GetSteers()))
	}
}

func TestASteerWithNoWordsIsRefusedByTheSystem(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	declared := declareJob(t, s, project, "build the transcripts page")

	_, err := s.RecordSteer(context.Background(), &quaycrewv1.RecordSteerRequest{Job: declared.GetId()})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a steer with no words answered %v, want InvalidArgument", err)
	}
}

func TestASteerAgainstAJobTheSystemDoesNotHoldIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	newProject(t, s)

	_, err := s.RecordSteer(context.Background(), &quaycrewv1.RecordSteerRequest{
		Job: "0123456789abcdef01234567", Text: "the workspace has no secrets",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("a steer against a job nobody has answered %v, want NotFound", err)
	}
}

// The whole point of the number is that the thing being scored cannot write it. A session runs
// under a job credential, and that credential reaches neither call.
func TestASessionMayNotRecordItsOwnSteer(t *testing.T) {
	held := auth.Grant{
		Job:   "0123456789abcdef01234567",
		Verbs: []string{role.VerbJobCreate, role.VerbJobRead, role.VerbJobStop, role.VerbJobAnswer},
	}
	for _, refused := range []struct {
		name   string
		method string
	}{
		{name: "recording one", method: quaycrewv1.ControlPlaneService_RecordSteer_FullMethodName},
		{name: "reading them back", method: quaycrewv1.ControlPlaneService_ListSteers_FullMethodName},
	} {
		t.Run(refused.name, func(t *testing.T) {
			err := controlplane.DeniedToJob(refused.method, nil, held)
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("a session %s answered %v, want PermissionDenied", refused.name, err)
			}
			if !strings.Contains(err.Error(), "job verbs") {
				t.Fatalf("the refusal does not say what a session may call: %v", err)
			}
		})
	}
}

// causedBy declares one job under another, the way a session running the parent declares work of its
// own: the parent is read from the credential rather than from the request.
func causedBy(t *testing.T, s *controlplane.Server, cause *quaycrewv1.Job, title string) *quaycrewv1.Job {
	t.Helper()
	if _, err := s.SetWorkspaceLimits(context.Background(), &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: cause.GetWorkspace(), MaxDeclared: 2},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	under := auth.WithGrant(context.Background(), auth.Grant{Job: cause.GetId(), Verbs: []string{role.VerbJobCreate}})
	declared, err := s.CreateJob(under, &quaycrewv1.CreateJobRequest{
		Project: cause.GetProject(), Title: title, Brief: "do it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.GetJob()
}

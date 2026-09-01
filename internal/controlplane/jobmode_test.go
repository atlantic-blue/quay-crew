package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A job that must push is held against the mode it would run in, at the moment of the write.
//
// Every way into a repository needs the network: the clone, the push, the pull request. The narrower
// modes ask a person before they run a network command, and nobody stands beside a dispatched job, so
// the approval never arrives. The system held both facts and never compared them, so it admitted the
// job, spent the session, and said so at the end.
//
// Through the control plane, because the refusal has to arrive before the row is written: a refusal
// that still records the job leaves the operator a job to go and stop.

// nothingWasWritten proves the refusal wrote no row.
func nothingWasWritten(t *testing.T, s *controlplane.Server, project string) {
	t.Helper()
	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(listed.GetJobs()) != 0 {
		t.Fatalf("the system holds %d jobs, and a refusal writes no row", len(listed.GetJobs()))
	}
}

// The refusal first. A rule that refused every job would pass a test that only proves a refusal, so
// the admissions are below.
func TestAJobThatWorksInARepositoryIsRefusedInAModeThatCannotReachIt(t *testing.T) {
	for _, mode := range []string{"plan", "edits"} {
		t.Run(mode, func(t *testing.T) {
			s := newServer(&model.FakeRunner{})
			_, project := newProject(t, s)

			err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
				Project: project, Title: "sort the listing", Brief: "make the listing sort by the clock it shows",
				Repository: "atlantic-blue/quay-crew", Mode: mode,
			})

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("the refusal is %v, want InvalidArgument", status.Code(err))
			}
			for _, phrase := range []string{"atlantic-blue/quay-crew", "mode " + mode, "--mode dangerous"} {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("the refusal says %q, want it to say %q", err, phrase)
				}
			}
			nothingWasWritten(t, s, project)
		})
	}
}

// The path nobody types a flag for, and the one this was reported on. A project holds the repository,
// so a job declared in it carries one without anybody saying so, and the mode is the system's.
func TestAJobThatTakesItsRepositoryFromItsProjectIsRefusedTheSameWay(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	record(t, s, project, "atlantic-blue/transcript", "public")

	err := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "sort the listing", Brief: "make the listing sort by the clock it shows",
	})

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("the refusal is %v, want InvalidArgument", status.Code(err))
	}
	// It says the mode came from the system rather than from the caller, because a person who typed no
	// mode has nothing to correct until they know where the mode came from.
	for _, phrase := range []string{"atlantic-blue/transcript", "names no mode", "edits", "--mode dangerous"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("the refusal says %q, want it to say %q", err, phrase)
		}
	}
	nothingWasWritten(t, s, project)
}

func TestAJobThatWorksInARepositoryIsDeclaredInTheModeThatReachesIt(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	created, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "sort the listing", Brief: "make the listing sort by the clock it shows",
		Repository: "atlantic-blue/quay-crew", Mode: "dangerous",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if got := created.GetJob().GetRepository(); got != "atlantic-blue/quay-crew" {
		t.Fatalf("the job works in %q, want atlantic-blue/quay-crew", got)
	}
	if got := created.GetJob().GetMode(); got != model.PermissionBypass {
		t.Fatalf("the job runs in %q, want %q", got, model.PermissionBypass)
	}
}

// The rule is the pair, not the mode. A job that works in no repository asks nothing of the network,
// so nothing about this narrows what a job may be.
func TestAJobThatWorksInNoRepositoryIsDeclaredInEveryMode(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	for _, mode := range []string{"", "plan", "edits", "dangerous"} {
		if _, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
			Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
			Mode: mode,
		}); err != nil {
			t.Errorf("a job in %q, working in no repository, was refused: %v", mode, err)
		}
	}
}

// The rule reads what this system will actually run, rather than a constant. A crew told to run its
// jobs in the mode that reaches the network admits the very job the default configuration refuses,
// and it is the same job declared the same way.
func TestACrewBornInTheModeThatReachesTheNetworkAdmitsTheSameJob(t *testing.T) {
	s := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{}, Provider: &sandbox.FakeProvider{},
		Secrets: secrets.NewMemory(), BirthPermissionMode: model.PermissionBypass,
	})
	_, project := newProject(t, s)
	record(t, s, project, "atlantic-blue/transcript", "public")

	created, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "sort the listing", Brief: "make the listing sort by the clock it shows",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if got := created.GetJob().GetRepository(); got != "atlantic-blue/transcript" {
		t.Fatalf("the job works in %q, want the project's atlantic-blue/transcript", got)
	}
}

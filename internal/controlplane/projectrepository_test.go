package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A project says where its work lands, and every job declared in it that names no repository of its
// own works in that one.
//
// The failure this is against is the one the acceptance run hit: a session told to push, holding a
// token that works, and no repository to push to. The system knew the workspace, the project and the
// token, and nowhere held the one fact that says where the work goes.

// record is the repository command, run against the server.
func record(t *testing.T, s *controlplane.Server, project, address, kind string) *quaycrewv1.Project {
	t.Helper()
	resp, err := s.SetProjectRepository(context.Background(), &quaycrewv1.SetProjectRepositoryRequest{
		Project: project, Repository: address, Visibility: kind,
	})
	if err != nil {
		t.Fatalf("SetProjectRepository: %v", err)
	}
	return resp.GetProject()
}

func TestAProjectRecordsWhereItsWorkLands(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	recorded := record(t, s, project, "atlantic-blue/transcript", "public")
	if recorded.GetRepository() != "atlantic-blue/transcript" {
		t.Fatalf("the project works in %q, want atlantic-blue/transcript", recorded.GetRepository())
	}

	read, err := s.GetProject(context.Background(), &quaycrewv1.GetProjectRequest{Id: project})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if read.GetProject().GetRepository() != "atlantic-blue/transcript" {
		t.Fatalf("read back %q, want atlantic-blue/transcript", read.GetProject().GetRepository())
	}
}

// The address somebody has in front of them is the one in their browser, so both spellings are taken
// and both are kept as one. The same rule a job's repository is held to, because it is one rule.
func TestTheAddressOfAProjectsRepositoryIsKeptAsAnOwnerAndAName(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	recorded := record(t, s, project, "https://github.com/atlantic-blue/transcript.git", "")
	if recorded.GetRepository() != "atlantic-blue/transcript" {
		t.Fatalf("the project works in %q, want atlantic-blue/transcript", recorded.GetRepository())
	}
}

// Saying nothing is public, because that is the cheaper of the two and the run had the operator say
// it out loud: "it should be a public repository so we can use the CI".
func TestARepositoryNobodyCalledAnythingIsPublic(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	if kind := record(t, s, project, "atlantic-blue/transcript", "").GetVisibility(); kind != "public" {
		t.Fatalf("a repository nobody called anything is %q, want public", kind)
	}
	if kind := record(t, s, project, "atlantic-blue/transcript", "private").GetVisibility(); kind != "private" {
		t.Fatalf("a repository called private is %q, want private", kind)
	}
}

func TestARepositoryThatIsNotAnOwnerAndANameIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	_, err := s.SetProjectRepository(context.Background(), &quaycrewv1.SetProjectRepositoryRequest{
		Project: project, Repository: "transcript",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("an address that is not an owner and a name was refused with %v, want invalid argument", err)
	}
	if !strings.Contains(err.Error(), "atlantic-blue/quay-crew") {
		t.Errorf("the refusal says %q, want it to say what to type instead", err)
	}
}

// A kind that is neither is refused rather than taken for the default. A forge has other kinds, and
// recording "internal" as public would be the system writing down a cost fact nobody told it.
func TestAKindOfRepositoryTheSystemDoesNotKnowIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	_, err := s.SetProjectRepository(context.Background(), &quaycrewv1.SetProjectRepositoryRequest{
		Project: project, Repository: "atlantic-blue/transcript", Visibility: "internal",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("an unknown kind was refused with %v, want invalid argument", err)
	}
	for _, wants := range []string{"public", "private"} {
		if !strings.Contains(err.Error(), wants) {
			t.Errorf("the refusal says %q, want it to say %q", err, wants)
		}
	}
}

func TestTheRepositoryOfAProjectTheSystemDoesNotHoldIsNotFound(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, err := s.SetProjectRepository(context.Background(), &quaycrewv1.SetProjectRepositoryRequest{
		Project: "ghost", Repository: "atlantic-blue/transcript",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("a repository on a project that does not exist was refused with %v, want not found", err)
	}
}

// The point of the record. A job declared with no repository of its own works in the project's, so
// the session doing it is told where to push without anybody passing the address again.
func TestAJobWorksInTheProjectsRepository(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	record(t, s, project, "atlantic-blue/transcript", "public")

	declared := declareJob(t, s, project, "read the electricity bill")

	if declared.GetRepository() != "atlantic-blue/transcript" {
		t.Fatalf("the job works in %q, want the project's atlantic-blue/transcript", declared.GetRepository())
	}
	// And the session doing it is told, which is the whole point: the line the system puts in front of
	// a session is what stops work that nobody can read.
	asked := job.Asked(&job.Job{Brief: "open the bill", Repository: declared.GetRepository()})
	if !strings.Contains(asked, "atlantic-blue/transcript") || !strings.Contains(asked, "pull request") {
		t.Errorf("the session doing it is asked %q, want it to name the repository and the pull request", asked)
	}
}

// A job that names its own repository keeps it. A project's is where the work lands by default, not
// a ceiling on where a job may work.
func TestAJobThatNamesItsOwnRepositoryKeepsIt(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	record(t, s, project, "atlantic-blue/transcript", "public")

	created, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "fix the listing", Brief: "make the listing sort by the clock it shows",
		Repository: "atlantic-blue/quay-crew",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if got := created.GetJob().GetRepository(); got != "atlantic-blue/quay-crew" {
		t.Fatalf("the job works in %q, want the atlantic-blue/quay-crew it named", got)
	}
}

// A project with no repository leaves a job as it was, which is every job declared before today.
func TestAJobInAProjectWithNoRepositoryClaimsNothing(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	if got := declareJob(t, s, project, "read the electricity bill").GetRepository(); got != "" {
		t.Fatalf("the job works in %q, and the project it is in works nowhere", got)
	}
}

// Correcting an address is not a statement about the bill, and it used to be read as one. An omitted
// kind cleared the kind the project held, so a project fell from private to public in the same
// command that fixed its address, and the answer said its pipeline minutes were free.
func TestAWriteWithNoKindKeepsTheKindTheProjectHolds(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	record(t, s, project, "atlantic-blue/transcript", "private")

	moved := record(t, s, project, "atlantic-blue/videos", "")
	if moved.GetVisibility() != "private" {
		t.Fatalf("the project is %q after a write that said no kind, want private", moved.GetVisibility())
	}
	// Read back out of the system, because what a call answered and what the system holds are two
	// things.
	read, err := s.GetProject(context.Background(), &quaycrewv1.GetProjectRequest{Id: project})
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if read.GetProject().GetVisibility() != "private" {
		t.Fatalf("the system holds %q, want private", read.GetProject().GetVisibility())
	}
	// And the kind is still the operator's to change, in the word that changes it.
	if back := record(t, s, project, "atlantic-blue/videos", "public").GetVisibility(); back != "public" {
		t.Fatalf("the kind could not be said back to %q, want public", back)
	}
}

// A project nobody has told is public, which is the case the keeping rule must not swallow: there is
// no kind to keep, and free minutes are the cheaper of the two.
func TestAWriteWithNoKindOnAProjectWithNoKindIsPublic(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	if kind := record(t, s, project, "atlantic-blue/transcript", "").GetVisibility(); kind != "public" {
		t.Fatalf("a project nobody has told is %q, want public", kind)
	}
}

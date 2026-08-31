//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/role"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The request through the control plane and a real database.
//
// What the unit tier proves is that the measure reads two pieces of text correctly. What only this
// tier proves is that the request survives the write, reaches a session two levels down, and that the
// answer a caller reads at the moment of the declaration says which words its brief dropped.

// The faithful brief comes first, and it is the case the whole feature turns on. A check that speaks
// about every brief puts a person in front of every job, which is the cost this system exists to
// remove.
func TestAFaithfulBriefIsDeclaredInSilenceThroughPostgres(t *testing.T) {
	s, kept := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	asked := "paste a youtube link and get the text back"
	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "the transcript page", Request: asked,
		Brief: "Build the page a reader pastes a YouTube link into. It fetches the transcript for that " +
			"link and renders the text back on the same page.",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if drifted := declared.GetDrifted(); drifted != "" {
		t.Fatalf("a brief that carries what was asked for was reported as drifted: %s", drifted)
	}

	// The row, not the call. A request that only exists in the answer is a request the session never
	// sees.
	read, err := kept.GetJob(ctx, declared.GetJob().GetId())
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if read.Request != asked {
		t.Fatalf("the database holds the request as %q, want %q", read.Request, asked)
	}
	if task := job.Asked(read); !strings.Contains(task, asked) {
		t.Fatalf("the session is asked %q, and it never sees what was asked for", task)
	}
}

// The brief that cost two days. It is declared, because refusing would stop work that may be right,
// and the answer names the words it dropped while the caller is still looking.
func TestABriefThatDropsWhatWasAskedForSaysSoThroughPostgres(t *testing.T) {
	s, kept := aSystemOnPostgres(t)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "the transcript page",
		Request: "paste a youtube link and get the text back",
		Brief: "Build a page that serves a transcript archive. The address reads /videos?id=<video id>, " +
			"and the video identifier is the key the store is read by.",
	})
	if err != nil {
		t.Fatalf("a drifted brief was refused rather than declared: %v", err)
	}
	drifted := declared.GetDrifted()
	if drifted == "" {
		t.Fatal("the brief that cost two days was declared in silence")
	}
	for _, word := range []string{"paste", "link", "text"} {
		if !strings.Contains(drifted, word) {
			t.Errorf("the answer does not name %q, which the brief never says: %s", word, drifted)
		}
	}
	// Declared, not refused. The job is a row like any other.
	read, err := kept.GetJob(ctx, declared.GetJob().GetId())
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if read.Phase != job.PhasePending {
		t.Fatalf("the job is in phase %q, want pending: this check refuses nothing", read.Phase)
	}
	// And the session is told which words it has to read the request again about.
	task := job.Asked(read)
	if !strings.Contains(task, "the brief says nothing about") {
		t.Fatalf("the session was never told its brief dropped words: %s", task)
	}
}

// A tree carries one request, and a session two levels down reads it without anybody typing it again.
func TestEveryJobUnderOneCarriesItsRequestThroughPostgres(t *testing.T) {
	s, kept := aSystemOnPostgres(t)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDepth: 2},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}

	asked := "can you make it so I paste a youtube link and get the text"
	root, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "the transcript page", Request: asked,
		Brief: "Build the page somebody pastes a youtube link into and reads the text back from.",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	child := declaredUnder(t, s, kept, project, root.GetJob().GetId(), "write the fetcher")
	grandchild := declaredUnder(t, s, kept, project, child, "write the renderer")

	for _, id := range []string{child, grandchild} {
		read, err := kept.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if read.Request != asked {
			t.Fatalf("job %s was asked for in the words %q, want %q", id, read.Request, asked)
		}
	}
	read, err := kept.GetJob(ctx, grandchild)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if task := job.Asked(read); !strings.Contains(task, asked) {
		t.Fatalf("the session two levels down is asked %q, and it never sees the request", task)
	}

	// A second request is refused rather than written, because a tree with two has none.
	_, _, err = s.PrepareJob(ctx, root.GetJob().GetId(), job.Declaration{
		Project: project, Title: "search the archive", Brief: "index every video by its identifier",
		Request: "build me a dashboard of the archive instead",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a child stating a second request was answered with %v", err)
	}
	if !strings.Contains(refusalOf(err), asked) {
		t.Fatalf("the refusal does not name the request the tree carries: %v", err)
	}
}

// A child is never measured against the request it inherited.
//
// A child builds one slice of the work, so it cannot carry the whole sentence, and measuring it would
// speak about every ordinary slice. A check that fires on ordinary work is the rule everybody learns
// to word around. So the reading is made on the request a declaration stated, and this is the test
// that says so: the same slice scores as drifted when the two texts are held against each other by
// hand, and the system says nothing about it.
func TestAChildIsNotMeasuredAgainstTheRequestItInherited(t *testing.T) {
	s, kept := aSystemOnPostgres(t)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDepth: 2},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	asked := "paste a youtube link and get the text back"
	root, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "the transcript page", Request: asked,
		Brief: "Build the page somebody pastes a youtube link into and reads the text back from.",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// The child is declared the way a session running the root declares one: through the same call,
	// carrying that job's credential, so the parent is read from the credential and never the request.
	slice := "add a database index on the identifier column"
	child, err := s.CreateJob(auth.WithGrant(ctx, auth.Grant{
		Job: root.GetJob().GetId(), Project: project, Verbs: []string{role.VerbJobCreate},
	}), &quaycrewv1.CreateJobRequest{Title: "add the index", Brief: slice})
	if err != nil {
		t.Fatalf("CreateJob under the root: %v", err)
	}
	if drifted := child.GetDrifted(); drifted != "" {
		t.Fatalf("an ordinary slice was reported as drifted from a request it never stated: %s", drifted)
	}
	// The slice really does drop the words, so the silence above is the rule and not the corpus.
	if job.Drifted(asked, slice) == "" {
		t.Fatal("this slice carries the request's words, so it proves nothing; pick another")
	}
	// And it carries the request all the same, because the session doing it still has to read what was
	// asked for.
	read, err := kept.GetJob(ctx, child.GetJob().GetId())
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if read.Request != asked {
		t.Fatalf("the child was asked for in the words %q, want %q", read.Request, asked)
	}
}

func refusalOf(err error) string {
	if s, ok := status.FromError(err); ok {
		return s.Message()
	}
	return err.Error()
}

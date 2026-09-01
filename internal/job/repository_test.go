package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A repository a caller typed and a repository a caller pasted are the same repository.
func TestARepositoryIsStoredAsOwnerAndName(t *testing.T) {
	for _, typed := range []string{
		"atlantic-blue/quay-crew",
		"  atlantic-blue/quay-crew  ",
		"https://github.com/atlantic-blue/quay-crew",
		"https://github.com/atlantic-blue/quay-crew/",
		"https://github.com/atlantic-blue/quay-crew.git",
		"git://example.com/atlantic-blue/quay-crew.git",
	} {
		d := declared()
		d.Repository = typed
		if got := d.Tidied().Repository; got != "atlantic-blue/quay-crew" {
			t.Errorf("%q is stored as %q, want atlantic-blue/quay-crew", typed, got)
		}
		if err := d.Validate(); err != nil {
			t.Errorf("%q was refused: %v", typed, err)
		}
	}
}

// A repository that is not an owner and a name is refused while the person who typed it is looking,
// because the alternative is a job that runs for an hour and stops on an address that was never
// going to match anything.
func TestARepositoryThatIsNotAnOwnerAndANameIsRefused(t *testing.T) {
	for _, typed := range []string{"quay-crew", "atlantic-blue/", "/quay-crew", "a/b/c", "atlantic blue/krewe"} {
		d := declared()
		d.Repository = typed
		err := d.Validate()
		if err == nil {
			t.Errorf("%q was accepted as a repository", typed)
			continue
		}
		if !strings.Contains(err.Error(), "atlantic-blue/quay-krewe") {
			t.Errorf("the refusal of %q says %q, want it to say what to type instead", typed, err)
		}
	}
}

func TestARepositoryOf201BytesIsRefused(t *testing.T) {
	d := declared()
	d.Repository = "owner/" + strings.Repeat("a", job.RepositoryLimit-len("owner/")+1)

	err := d.Validate()
	if err == nil {
		t.Fatal("a repository above the ceiling was accepted")
	}
	if !strings.Contains(err.Error(), "200") {
		t.Fatalf("the refusal says %q, want it to name the ceiling", err)
	}
}

// A job that names no repository claims nothing, which is every job declared before today.
func TestNoRepositoryIsNoClaim(t *testing.T) {
	if err := declared().Validate(); err != nil {
		t.Fatalf("a declaration naming no repository was refused: %v", err)
	}
	if got := job.PullRequestIn("", "https://github.com/atlantic-blue/quay-crew/pull/1"); got != "" {
		t.Fatalf("a job naming no repository found the address %q", got)
	}
}

func TestTheAddressIsReadOutOfAnAnswer(t *testing.T) {
	const want = "https://github.com/atlantic-blue/quay-crew/pull/454"
	for _, answer := range []string{
		want,
		"I opened " + want,
		"Opened " + want + ".",
		"Opened it (" + want + ") and left it for review.",
		"Done.\n\n" + want + "\n\nThe branch is pushed.",
		"opened HTTPS://GITHUB.COM/Atlantic-Blue/Quay-Crew/pull/454",
	} {
		got := job.PullRequestIn("atlantic-blue/quay-crew", answer)
		if !strings.EqualFold(got, want) {
			t.Errorf("the address read out of %q is %q, want %s", answer, got, want)
		}
	}
}

// The address of somebody else's pull request is not where this job's work went. A job that took
// any pull request address would be satisfied by a session that read one.
func TestAnAddressAgainstAnotherRepositoryIsNotThisJobsPullRequest(t *testing.T) {
	for _, answer := range []string{
		"I read https://github.com/someone-else/other-repo/pull/12",
		"https://github.com/atlantic-blue/other-repo/pull/12",
		"https://github.com/atlantic-blue/quay-crew/issues/453",
		"https://github.com/atlantic-blue/quay-crew/pull/",
		"I pushed the branch and did not open one",
	} {
		if got := job.PullRequestIn("atlantic-blue/quay-crew", answer); got != "" {
			t.Errorf("%q was read as the pull request %q", answer, got)
		}
	}
}

// A session doing a job that names a repository is told how the job ends, and told not to merge.
// The merge is the gate: a push applies nothing and a merge runs the pipeline.
func TestASessionIsToldTheJobEndsInAPullRequest(t *testing.T) {
	asked := job.Asked(&job.Job{
		Brief: "make the listing sort by the clock it shows", Repository: "atlantic-blue/quay-crew",
	})
	if !strings.Contains(asked, "make the listing sort by the clock it shows") {
		t.Fatalf("the session is asked %q, want it to carry the brief", asked)
	}
	for _, phrase := range []string{"atlantic-blue/quay-crew", "pull request", "Do not merge"} {
		if !strings.Contains(asked, phrase) {
			t.Errorf("the session is asked %q, want it to say %q", asked, phrase)
		}
	}
}

// A job that names no repository is told nothing about one. What the system adds to a brief it adds
// for a reason the job declared, and a job that declared no repository has no such reason.
func TestAJobNamingNoRepositoryIsToldNothingAboutAPullRequest(t *testing.T) {
	brief := "open the bill and say when it is due"
	asked := job.Asked(&job.Job{Brief: brief})
	if !strings.Contains(asked, brief) {
		t.Fatalf("the session is asked %q, want its brief", asked)
	}
	if strings.Contains(asked, "pull request") {
		t.Fatalf("the session is asked %q, and this job names no repository", asked)
	}
}

// A repository is reached over the network, and a mode that asks a person before it runs a network
// command cannot reach one. Nobody stands beside a dispatched job, so the approval never arrives: the
// system used to admit the pair, spend the session, and say so at the end.
//
// The refusal first, because a rule that refused everything would pass every test that only proves a
// refusal. The admission is below it.
func TestAJobThatWorksInARepositoryIsRefusedInAModeThatCannotReachIt(t *testing.T) {
	for _, mode := range []string{"plan", "edits", "acceptEdits"} {
		t.Run(mode, func(t *testing.T) {
			d := declared()
			d.Repository = "atlantic-blue/quay-crew"
			d.Mode = mode

			err := d.Validate()
			if err == nil {
				t.Fatalf("a job in %s was admitted, and it works in a repository it cannot reach", mode)
			}
			// All three, because a refusal that says only that something is wrong leaves the operator
			// guessing: which repository, which mode, and what to type instead.
			for _, phrase := range []string{"atlantic-blue/quay-crew", "needs the network", "--mode dangerous"} {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("the refusal says %q, want it to say %q", err, phrase)
				}
			}
		})
	}
}

func TestAJobThatWorksInARepositoryIsDeclaredInTheModeThatReachesIt(t *testing.T) {
	for _, mode := range []string{"dangerous", "bypassPermissions"} {
		t.Run(mode, func(t *testing.T) {
			d := declared()
			d.Repository = "atlantic-blue/quay-crew"
			d.Mode = mode

			if err := d.Validate(); err != nil {
				t.Fatalf("a job in %s was refused: %v", mode, err)
			}
		})
	}
}

// The rule is the pair, not the mode. A job that works in no repository asks nothing of the network,
// so every mode still declares one and nothing about this change narrows what a job may be.
func TestAJobThatWorksInNoRepositoryIsDeclaredInEveryMode(t *testing.T) {
	for _, mode := range []string{"", "plan", "edits", "dangerous", "acceptEdits", "bypassPermissions"} {
		t.Run(mode, func(t *testing.T) {
			d := declared()
			d.Mode = mode

			if err := d.Validate(); err != nil {
				t.Fatalf("a job in %q, working in no repository, was refused: %v", mode, err)
			}
		})
	}
}

// A job that names no mode is admitted here and held again at the control plane, which is the only
// place that holds what an unnamed mode runs in. Refusing it here would refuse every job on a crew
// already configured to run its work in the mode that can push.
func TestAJobThatNamesNoModeIsLeftToTheSystem(t *testing.T) {
	d := declared()
	d.Repository = "atlantic-blue/quay-crew"

	if err := d.Validate(); err != nil {
		t.Fatalf("a job that names no mode was refused: %v", err)
	}
}

// The address is held to its shape first, so a job that got both wrong is told about the address it
// typed rather than about a mode it can do nothing with until the address is right.
func TestARepositoryThatIsNotAnOwnerAndANameIsRefusedBeforeTheMode(t *testing.T) {
	d := declared()
	d.Repository = "quay-crew"
	d.Mode = "edits"

	err := d.Validate()
	if err == nil {
		t.Fatal("a repository that is not an owner and a name was admitted")
	}
	if strings.Contains(err.Error(), "--mode") {
		t.Fatalf("the refusal says %q, want it to be about the address", err)
	}
}

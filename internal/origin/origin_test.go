package origin_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/origin"
)

// Where a directory came from, read from real repositories rather than from a double.
//
// A double would agree with whatever this package decided git says, which is the one thing worth
// proving: the answers here decide whether an operator is told a role can be reviewed, and a wrong
// yes is worse than no answer at all.

func TestADirectoryInARepositoryComesBackWithItsRemoteCommitAndPath(t *testing.T) {
	repository := aRepository(t)
	dir := aDirectoryIn(t, repository, "roles/orchestrator")
	commitEverything(t, repository)
	pushed(t, repository)

	read := origin.Of(dir)
	if read.Repository != "github.com/atlantic-blue/quay-crew" {
		t.Errorf("it came from %q, and the remote is github.com/atlantic-blue/quay-crew", read.Repository)
	}
	if read.Commit != head(t, repository) {
		t.Errorf("it came back at commit %q, and the checkout is at %q", read.Commit, head(t, repository))
	}
	// The path inside the repository, not the path on this machine: a reviewer opens the repository,
	// and where the checkout happened to sit is nobody else's business.
	if read.Path != "roles/orchestrator" {
		t.Errorf("it came back from %q, want roles/orchestrator", read.Path)
	}
	if read.Dirty {
		t.Error("it came back dirty, and everything in it is committed")
	}
	if read.Unpushed {
		t.Error("it came back unpushed, and the commit is on a remote branch")
	}
	if !read.Reviewable() {
		t.Errorf("a committed, pushed directory is not reviewable: %s", read.Line())
	}
}

// The failure the whole package exists for: files in a folder on somebody's disk, which no pull
// request ever touched and nobody else can open.
func TestADirectoryOutsideARepositoryIsSaidToBeLoose(t *testing.T) {
	dir := t.TempDir()

	read := origin.Of(dir)
	if read.Repository != "" {
		t.Errorf("it claims to come from %q, and it is not in a repository", read.Repository)
	}
	if read.Commit != "" {
		t.Errorf("it claims commit %q, and it is not in a repository", read.Commit)
	}
	if read.Path != dir {
		t.Errorf("it came back from %q, and it was read from %q", read.Path, dir)
	}
	if read.Reviewable() {
		t.Error("a loose directory came back reviewable, so nothing would warn the operator")
	}
	if said := read.Line(); !strings.Contains(said, "not in a repository") || !strings.Contains(said, dir) {
		t.Errorf("the line does not say where it is and that it is loose: %q", said)
	}
}

func TestUncommittedChangesAreSaidAgainstTheDirectoryTheyAreIn(t *testing.T) {
	repository := aRepository(t)
	dir := aDirectoryIn(t, repository, "roles/orchestrator")
	commitEverything(t, repository)
	pushed(t, repository)
	write(t, filepath.Join(dir, "ROLE.md"), "Write the product yourself.\n")

	read := origin.Of(dir)
	if !read.Dirty {
		t.Fatal("an edited directory came back clean, so the commit named is not what was imported")
	}
	if read.Reviewable() {
		t.Error("an edited directory came back reviewable")
	}
	if said := read.Line(); !strings.Contains(said, "uncommitted") {
		t.Errorf("the line does not say the files are uncommitted: %q", said)
	}
}

// A change elsewhere in the repository is not this directory's business. Without the path, every
// import from a working checkout would be reported dirty and the warning would mean nothing.
func TestAChangeElsewhereInTheRepositoryLeavesTheDirectoryClean(t *testing.T) {
	repository := aRepository(t)
	dir := aDirectoryIn(t, repository, "roles/orchestrator")
	aDirectoryIn(t, repository, "roles/releaser")
	commitEverything(t, repository)
	pushed(t, repository)
	write(t, filepath.Join(repository, "roles/releaser/ROLE.md"), "Release it twice.\n")

	if read := origin.Of(dir); read.Dirty {
		t.Errorf("the orchestrator came back dirty because the releaser was edited: %s", read.Line())
	}
}

// A directory that was never committed at all is the same failure as a loose one wearing a
// repository's name: git holds none of it, so a reviewer opening the repository finds nothing.
func TestADirectoryGitHasNeverSeenIsDirty(t *testing.T) {
	repository := aRepository(t)
	dir := aDirectoryIn(t, repository, "roles/orchestrator")
	commitEverything(t, repository)
	pushed(t, repository)
	uncommitted := aDirectoryIn(t, repository, "roles/releaser")
	_ = dir

	if read := origin.Of(uncommitted); !read.Dirty {
		t.Errorf("a directory git has never seen came back clean: %s", read.Line())
	}
}

// Committed and never pushed. The operator asked for the work to be in code and pushed, and a
// commit sitting on one laptop is as invisible as a folder on one.
func TestACommitOnNoRemoteBranchIsSaid(t *testing.T) {
	repository := aRepository(t)
	dir := aDirectoryIn(t, repository, "roles/orchestrator")
	commitEverything(t, repository)

	read := origin.Of(dir)
	if !read.Unpushed {
		t.Fatal("a commit on no remote branch came back pushed")
	}
	if read.Dirty {
		t.Error("it came back dirty, and everything in it is committed")
	}
	if read.Reviewable() {
		t.Error("an unpushed commit came back reviewable")
	}
	if said := read.Line(); !strings.Contains(said, "remote branch") {
		t.Errorf("the line does not say the commit reached no remote branch: %q", said)
	}
}

// Both reasons at once, and the line names both. A sentence that stops at the first one sends the
// operator to fix half of it and import again into the same warning.
func TestALineNamesEveryReasonNobodyElseCanReadIt(t *testing.T) {
	repository := aRepository(t)
	dir := aDirectoryIn(t, repository, "roles/orchestrator")
	commitEverything(t, repository)
	write(t, filepath.Join(dir, "ROLE.md"), "Write the product yourself.\n")

	said := origin.Of(dir).Line()
	if !strings.Contains(said, "uncommitted") || !strings.Contains(said, "remote branch") {
		t.Errorf("the line names one reason and there are two: %q", said)
	}
}

// A role imported before the system recorded any of this. It is not a loose directory and saying so
// would be an accusation the system cannot support.
func TestAnOriginNothingRecordedSaysThatAndNotThatItIsLoose(t *testing.T) {
	var nothing origin.Origin

	if nothing.Reviewable() {
		t.Error("an origin holding nothing came back reviewable")
	}
	if said := nothing.Line(); !strings.Contains(said, "not recorded") {
		t.Errorf("it should say nothing was recorded, and it says: %q", said)
	}
	if said := nothing.Line(); strings.Contains(said, "not in a repository") {
		t.Errorf("it accuses a role of being loose on no evidence: %q", said)
	}
}

// One address per repository, whichever way the remote is written. Two spellings of one repository
// read as two places to go and look.
func TestARemoteAddressIsWrittenTheOneWay(t *testing.T) {
	for _, one := range []struct{ remote, want string }{
		{"https://github.com/atlantic-blue/quay-crew.git", "github.com/atlantic-blue/quay-crew"},
		{"https://github.com/atlantic-blue/quay-crew", "github.com/atlantic-blue/quay-crew"},
		{"git@github.com:atlantic-blue/quay-crew.git", "github.com/atlantic-blue/quay-crew"},
		{"ssh://git@github.com/atlantic-blue/quay-crew.git", "github.com/atlantic-blue/quay-crew"},
		{"https://token@github.com/atlantic-blue/quay-crew.git", "github.com/atlantic-blue/quay-crew"},
		{"https://gitlab.example.com/team/thing.git/", "gitlab.example.com/team/thing"},
	} {
		if got := origin.Address(one.remote); got != one.want {
			t.Errorf("%s reads as %q, want %q", one.remote, got, one.want)
		}
	}
}

// A remote address that carries a credential must not travel into the system, where a listing prints
// it and a database keeps it.
func TestACredentialInARemoteAddressIsNotKept(t *testing.T) {
	got := origin.Address("https://someone:ghp_notarealtoken@github.com/atlantic-blue/quay-crew.git")
	if strings.Contains(got, "ghp_notarealtoken") || strings.Contains(got, "someone") {
		t.Errorf("the address kept the credential: %q", got)
	}
}

func aRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--initial-branch=main")
	git(t, dir, "config", "user.email", "system@example.com")
	git(t, dir, "config", "user.name", "system")
	git(t, dir, "remote", "add", "origin", "https://github.com/atlantic-blue/quay-crew.git")
	return dir
}

func aDirectoryIn(t *testing.T, repository, at string) string {
	t.Helper()
	dir := filepath.Join(repository, filepath.FromSlash(at))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "ROLE.md"), "Name the children and review what they write.\n")
	return dir
}

func write(t *testing.T, at, body string) {
	t.Helper()
	if err := os.WriteFile(at, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitEverything(t *testing.T, repository string) {
	t.Helper()
	git(t, repository, "add", "-A")
	git(t, repository, "commit", "-m", "the roles this build ships")
}

// pushed puts the commit on a remote branch without a remote to push to, which is what a push leaves
// behind and all this package reads.
func pushed(t *testing.T, repository string) {
	t.Helper()
	git(t, repository, "update-ref", "refs/remotes/origin/main", "HEAD")
}

func head(t *testing.T, repository string) string {
	t.Helper()
	return git(t, repository, "rev-parse", "HEAD")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

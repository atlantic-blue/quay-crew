package promise

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These run real git over a real repository rather than a double, because what is being tested is
// the reading of git's output. A double would answer in the shape this package already believes git
// speaks, which is the belief worth checking: -z, a rename carrying two paths, and a path with a
// space in it that git quotes in every other form.

// repo is a repository made for one test, with the identity given here rather than taken from
// whoever runs the tests, and signing off, because continuous integration holds no key.
type repository struct {
	t   *testing.T
	dir string
}

func newRepository(t *testing.T) *repository {
	t.Helper()
	r := &repository{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main", ".")
	return r
}

func (r *repository) git(args ...string) string {
	r.t.Helper()
	command := exec.Command("git", append([]string{
		"-C", r.dir,
		"-c", "user.name=quay",
		"-c", "user.email=quay@example.invalid",
		"-c", "commit.gpgsign=false",
		// git keeps house after a commit, and it detaches that work, so it outlives the command
		// that started it and writes packs into .git/objects while the temporary directory is being
		// removed. An unscheduled run repacks once the objects/17 shard holds two objects, which a
		// repository this small reaches by luck. The first pair turns the housekeeping off, and the
		// second says that whatever a later git decides to do is done before the command returns.
		"-c", "maintenance.auto=false",
		"-c", "gc.auto=0",
		"-c", "maintenance.autoDetach=false",
		"-c", "gc.autoDetach=false",
	}, args...)...)
	out, err := command.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func (r *repository) write(name, body string) {
	r.t.Helper()
	at := filepath.Join(r.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repository) remove(name string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, filepath.FromSlash(name))); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repository) commit(message string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-qm", message)
}

// found says what a set of files holds for one path, so a case reads as a sentence.
func found(files []File, path string) string {
	for _, file := range files {
		if file.Path == path {
			return string(file.Status)
		}
	}
	return "not there"
}

func TestChangedReadsWhatABranchDid(t *testing.T) {
	r := newRepository(t)
	r.write("internal/job/waiting.go", "what was there before\n")
	r.write("features/job.feature", "Feature: jobs\n")
	r.write("docs/a note with spaces.md", "quoted in every form but -z\n")
	r.write("internal/job/gone.go", "about to be deleted\n")
	r.write("internal/job/old.go", "about to be renamed\n")
	r.commit("what the change starts from")

	r.git("switch", "-q", "-c", "change")
	r.write("internal/job/waiting.go", "what was there before\nand what the change did\n")
	r.write("changelog.d/486-a-check.md", "**A check reads the diff.**\n")
	r.write("docs/a note with spaces.md", "quoted in every form but -z\nand edited\n")
	r.remove("internal/job/gone.go")
	r.git("mv", "internal/job/old.go", "internal/job/new.go")
	r.commit("the change")

	files, err := Changed(r.dir, "main", "HEAD")
	if err != nil {
		t.Fatalf("reading the change: %v", err)
	}
	// Six, because the rename is read as two: the path it came from and the path it went to.
	if len(files) != 6 {
		t.Fatalf("read %d files, want 6: %+v", len(files), files)
	}
	for path, want := range map[string]string{
		"internal/job/waiting.go":    string(Modified),
		"changelog.d/486-a-check.md": string(Added),
		"docs/a note with spaces.md": string(Modified),
		"internal/job/gone.go":       string(Deleted),
		"internal/job/old.go":        string(Deleted),
		"internal/job/new.go":        string(Added),
	} {
		if got := found(files, path); got != want {
			t.Errorf("%s came back %s, want %s", path, got, want)
		}
	}
}

// TestTheWholeCheckOverARealBranch is the check itself against a real diff rather than a hand written
// list of files: git reads the branch, and the answer is the refusal a person would see.
func TestTheWholeCheckOverARealBranch(t *testing.T) {
	for _, one := range []struct {
		name string
		// change is what the branch does, as paths to write. deletes is what it removes.
		change  []string
		deletes []string
		body    string
		want    []string
	}{
		{
			name:   "behaviour with both promises kept",
			change: []string{"internal/job/waiting.go", "changelog.d/486-a-check.md", "features/promises.feature"},
		},
		{
			name:   "behaviour with neither",
			change: []string{"internal/job/waiting.go"},
			want:   []string{ChangelogEntry, Scenario},
		},
		{
			name:   "a stated reason stands in for the scenario",
			change: []string{"internal/job/waiting.go", "changelog.d/486-a-check.md"},
			body:   "No scenario: the behaviour is unchanged, this moves it between packages",
		},
		{
			name:    "the change that only deletes the last scenario",
			change:  []string{"internal/job/waiting.go", "changelog.d/486-a-check.md"},
			deletes: []string{"features/promises.feature"},
			want:    []string{Scenario},
		},
		{
			name:   "documentation on its own",
			change: []string{"docs/ARCHITECTURE.md"},
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			r := newRepository(t)
			r.write("README.md", "a repository made for one test\n")
			for _, path := range one.deletes {
				r.write(path, "what was there before the change\n")
			}
			r.commit("what the change starts from")

			r.git("switch", "-q", "-c", "change")
			for at, path := range one.change {
				r.write(path, fmt.Sprintf("what the change wrote, %d\n", at))
			}
			for _, path := range one.deletes {
				r.remove(path)
			}
			r.commit("the change")

			files, err := Changed(r.dir, "main", "HEAD")
			if err != nil {
				t.Fatalf("reading the change: %v", err)
			}
			got := missing(Check(Change{Files: files, Body: one.body}))
			if len(got) != len(one.want) {
				t.Fatalf("the check asks for %v, want %v (it read %+v)", got, one.want, files)
			}
			for at := range got {
				if got[at] != one.want[at] {
					t.Fatalf("the check asks for %v, want %v", got, one.want)
				}
			}
		})
	}
}

// TestARangeThatCannotBeReadIsAnError, rather than an empty answer that reads as a change with
// nothing in it and keeps every promise there is.
func TestARangeThatCannotBeReadIsAnError(t *testing.T) {
	r := newRepository(t)
	r.write("README.md", "a repository made for one test\n")
	r.commit("the only commit")

	if _, err := Changed(r.dir, "origin/nowhere", "HEAD"); err == nil {
		t.Fatal("a base ref that does not exist came back with no error, so a wrong base reads as an empty change")
	}
}

// TestWhatTheBaseDidWhileTheChangeWasOpenIsNotTheChange.
//
// The range is base...head rather than base..head, and the difference only shows once the base moves.
// Two dots compare the two trees, so a commit that landed on main while the branch was open comes
// back as a file this change deleted. The check would then refuse a change for touching behaviour
// somebody else wrote, name a file the author never opened, and be right about none of it.
func TestWhatTheBaseDidWhileTheChangeWasOpenIsNotTheChange(t *testing.T) {
	r := newRepository(t)
	r.write("README.md", "a repository made for one test\n")
	r.commit("what the change starts from")

	r.git("switch", "-q", "-c", "change")
	r.write("internal/job/waiting.go", "what the change did\n")
	r.write("changelog.d/486-a-check.md", "**A check reads the diff.**\n")
	r.write("features/promises.feature", "Feature: promises\n")
	r.commit("the change")

	// Somebody else's work lands on the base while this change is open.
	r.git("switch", "-q", "main")
	r.write("internal/room/view.go", "nothing to do with the change\n")
	r.commit("what somebody else shipped meanwhile")

	files, err := Changed(r.dir, "main", "change")
	if err != nil {
		t.Fatalf("reading the change: %v", err)
	}
	if got := found(files, "internal/room/view.go"); got != "not there" {
		t.Errorf("the change is blamed for internal/room/view.go, which came back %s: %+v", got, files)
	}
	if len(files) != 3 {
		t.Fatalf("read %d files, want the 3 the change made: %+v", len(files), files)
	}
	if findings := Check(Change{Files: files}); len(findings) != 0 {
		t.Fatalf("the check refuses a change that kept both promises: %v", missing(findings))
	}
}

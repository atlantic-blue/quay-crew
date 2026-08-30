package promise

import (
	"strings"
	"testing"
)

// edited, added and removed build the files a change touched, so a table below reads as a change
// rather than as a list of structs.
func edited(paths ...string) []File  { return listed(Modified, paths) }
func added(paths ...string) []File   { return listed(Added, paths) }
func removed(paths ...string) []File { return listed(Deleted, paths) }

func listed(status Status, paths []string) []File {
	files := make([]File, 0, len(paths))
	for _, path := range paths {
		files = append(files, File{Path: path, Status: status})
	}
	return files
}

func join(sets ...[]File) []File {
	var all []File
	for _, set := range sets {
		all = append(all, set...)
	}
	return all
}

// missing is the promises a set of findings names, so a case says what it expects in the words the
// refusal uses.
func missing(findings []Finding) []string {
	names := make([]string, 0, len(findings))
	for _, finding := range findings {
		names = append(names, finding.Promise)
	}
	return names
}

func TestWhatAChangeIsAskedFor(t *testing.T) {
	for _, one := range []struct {
		name  string
		files []File
		body  string
		want  []string
	}{
		{
			name:  "behaviour with both promises kept is asked for nothing",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md", "features/promises.feature")),
		},
		{
			name:  "behaviour with neither is asked for both",
			files: edited("internal/job/waiting.go"),
			want:  []string{ChangelogEntry, Scenario},
		},
		{
			name:  "behaviour with no changelog entry",
			files: join(edited("internal/job/waiting.go"), added("features/promises.feature")),
			want:  []string{ChangelogEntry},
		},
		{
			name:  "behaviour with no scenario",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md")),
			want:  []string{Scenario},
		},
		{
			name:  "an existing feature file gaining a scenario counts",
			files: join(edited("internal/job/waiting.go", "features/job.feature"), added("changelog.d/486-a-check.md")),
		},
		{
			// The change that deletes behaviour is the change a reader most needs told about.
			name:  "deleting behaviour is a behaviour change",
			files: removed("internal/job/waiting.go"),
			want:  []string{ChangelogEntry, Scenario},
		},
		{
			name:  "a test on its own is not behaviour",
			files: edited("internal/job/waiting_test.go", "features/job_steps_test.go"),
		},
		{
			name:  "documentation on its own is not behaviour",
			files: edited("docs/ARCHITECTURE.md", "README.md", ".github/workflows/ci.yml", "Makefile"),
		},
		{
			// buf writes it, and the proto it comes from is already counted.
			name:  "generated code on its own is not behaviour",
			files: edited("gen/quaycrew/v1/crew.pb.go"),
		},
		{
			name:  "a contract is behaviour",
			files: edited("proto/quaycrew/v1/crew.proto"),
			want:  []string{ChangelogEntry, Scenario},
		},
		{
			name:  "a hook is behaviour, and it is its own module",
			files: edited("hooks/merge-gate/gate.go"),
			want:  []string{ChangelogEntry, Scenario},
		},
		{
			name:  "the README next to the fragments is not a fragment",
			files: join(edited("internal/job/waiting.go"), edited("changelog.d/README.md"), added("features/promises.feature")),
			want:  []string{ChangelogEntry},
		},
		{
			name:  "deleting the last scenario is not carrying one",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md"), removed("features/promises.feature")),
			want:  []string{Scenario},
		},
		{
			// A release deletes every fragment in the commit that assembles them, and it must not
			// read as a change that wrote one.
			name:  "deleting a fragment is not writing one",
			files: join(edited("internal/job/waiting.go"), removed("changelog.d/480-fragments.md"), added("features/promises.feature")),
			want:  []string{ChangelogEntry},
		},
		{
			name:  "a stated reason stands in for the scenario",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md")),
			body:  "**What.** Moves the rule between packages.\n\nNo scenario: the behaviour is unchanged, so the ones in job.feature already hold it up",
			want:  nil,
		},
		{
			name:  "a stated reason stands in for the changelog entry",
			files: join(edited("internal/job/waiting.go"), added("features/promises.feature")),
			body:  "No changelog entry: this renames a field nobody outside the package reads",
			want:  nil,
		},
		{
			name:  "one reason does not stand in for the other",
			files: edited("internal/job/waiting.go"),
			body:  "No scenario: the behaviour is unchanged, this only moves code",
			want:  []string{ChangelogEntry},
		},
		{
			name:  "a bullet is how a body usually writes the line",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md")),
			body:  "- No scenario: every path here is already covered by job.feature",
			want:  nil,
		},
		{
			name:  "case is not the point",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md")),
			body:  "no SCENARIO: the behaviour is unchanged, this only moves code",
			want:  nil,
		},
		{
			name:  "one word after the colon is silence with a colon in front",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md")),
			body:  "No scenario: none",
			want:  []string{Scenario},
		},
		{
			name:  "nothing after the colon is silence too",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md")),
			body:  "No scenario:",
			want:  []string{Scenario},
		},
		{
			// The words have to be the line rather than anywhere in a sentence, or a body that
			// mentions the rule while describing it lets itself off.
			name:  "the words in the middle of a sentence are not the line",
			files: join(edited("internal/job/waiting.go"), added("changelog.d/486-a-check.md")),
			body:  "This is the check that refuses a change with No scenario: it reads the diff",
			want:  []string{Scenario},
		},
		{
			name:  "a change that touches nothing at all is asked for nothing",
			files: nil,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			got := missing(Check(Change{Files: one.files, Body: one.body}))
			if strings.Join(got, ",") != strings.Join(one.want, ",") {
				t.Fatalf("the check asks for %v, want %v", got, one.want)
			}
		})
	}
}

// TestARefusalSaysWhatToDo: a check that only says no is a check somebody works around. The refusal
// has to name what made this a behaviour change, what to write, and the line that says why not.
func TestARefusalSaysWhatToDo(t *testing.T) {
	findings := Check(Change{Files: edited("internal/job/waiting.go")})
	if len(findings) != 2 {
		t.Fatalf("a change with neither promise gets %d findings, want 2", len(findings))
	}
	said := findings[0].String() + findings[1].String()
	for _, want := range []string{
		"internal/job/waiting.go",
		"changelog.d/<issue>-<words-joined-with-hyphens>.md",
		"features/<capability>.feature",
		"No changelog entry: <why>",
		"No scenario: <why>",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal never says %q:\n%s", want, said)
		}
	}
}

// TestOnlyTheFilesThatPutItUnderTheRuleAreNamed keeps the refusal to the reason: naming every file a
// change touched would bury the two Go files under a hundred lines of the change itself.
func TestOnlyTheFilesThatPutItUnderTheRuleAreNamed(t *testing.T) {
	findings := Check(Change{Files: join(
		edited("internal/job/waiting.go", "docs/ARCHITECTURE.md", "internal/job/waiting_test.go"),
		added("changelog.d/486-a-check.md"),
	)})
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want the scenario alone", len(findings))
	}
	if got := findings[0].Because; len(got) != 1 || got[0] != "internal/job/waiting.go" {
		t.Fatalf("the refusal blames %v, want internal/job/waiting.go alone", got)
	}
}

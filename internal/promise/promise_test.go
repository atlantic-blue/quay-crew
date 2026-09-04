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
			name:  "behaviour with a scenario is asked for nothing",
			files: join(edited("internal/session/waiting.go"), added("features/promises.feature")),
		},
		{
			name:  "behaviour with none is asked for one",
			files: edited("internal/session/waiting.go"),
			want:  []string{Scenario},
		},
		{
			name:  "behaviour with no scenario",
			files: edited("internal/session/waiting.go"),
			want:  []string{Scenario},
		},
		{
			name:  "an existing feature file gaining a scenario counts",
			files: edited("internal/session/waiting.go", "features/session.feature"),
		},
		{
			// The change that deletes behaviour is the change a reader most needs told about.
			name:  "deleting behaviour is a behaviour change",
			files: removed("internal/session/waiting.go"),
			want:  []string{Scenario},
		},
		{
			name:  "a test on its own is not behaviour",
			files: edited("internal/session/waiting_test.go", "features/session_steps_test.go"),
		},
		{
			name:  "documentation on its own is not behaviour",
			files: edited("docs/ARCHITECTURE.md", "README.md", ".github/workflows/ci.yml", "Makefile"),
		},
		{
			// buf writes it, and the proto it comes from is already counted.
			name:  "generated code on its own is not behaviour",
			files: edited("gen/quaycrew/v1/system.pb.go"),
		},
		{
			name:  "a contract is behaviour",
			files: edited("proto/quaycrew/v1/system.proto"),
			want:  []string{Scenario},
		},
		{
			name:  "a hook is behaviour, and it is its own module",
			files: edited("hooks/merge-gate/gate.go"),
			want:  []string{Scenario},
		},
		{
			name:  "deleting the last scenario is not carrying one",
			files: join(edited("internal/session/waiting.go"), removed("features/promises.feature")),
			want:  []string{Scenario},
		},
		{
			name:  "a stated reason stands in for the scenario",
			files: edited("internal/session/waiting.go"),
			body:  "**What.** Moves the rule between packages.\n\nNo scenario: the behaviour is unchanged, so the ones in session.feature already hold it up",
			want:  nil,
		},
		{
			name:  "a bullet is how a body usually writes the line",
			files: edited("internal/session/waiting.go"),
			body:  "- No scenario: every path here is already covered by session.feature",
			want:  nil,
		},
		{
			name:  "case is not the point",
			files: edited("internal/session/waiting.go"),
			body:  "no SCENARIO: the behaviour is unchanged, this only moves code",
			want:  nil,
		},
		{
			name:  "one word after the colon is silence with a colon in front",
			files: edited("internal/session/waiting.go"),
			body:  "No scenario: none",
			want:  []string{Scenario},
		},
		{
			name:  "nothing after the colon is silence too",
			files: edited("internal/session/waiting.go"),
			body:  "No scenario:",
			want:  []string{Scenario},
		},
		{
			// The words have to be the line rather than anywhere in a sentence, or a body that
			// mentions the rule while describing it lets itself off.
			name:  "the words in the middle of a sentence are not the line",
			files: edited("internal/session/waiting.go"),
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
	findings := Check(Change{Files: edited("internal/session/waiting.go")})
	if len(findings) != 1 {
		t.Fatalf("a change with no scenario gets %d findings, want 1", len(findings))
	}
	said := findings[0].String()
	for _, want := range []string{
		"internal/session/waiting.go",
		"features/<capability>.feature",
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
	findings := Check(Change{Files: edited(
		"internal/session/waiting.go", "docs/ARCHITECTURE.md", "internal/session/waiting_test.go",
	)})
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want the scenario alone", len(findings))
	}
	if got := findings[0].Because; len(got) != 1 || got[0] != "internal/session/waiting.go" {
		t.Fatalf("the refusal blames %v, want internal/session/waiting.go alone", got)
	}
}

// TestAnExampleIsNotTheLine.
//
// A body that explains the rule, or quotes the refusal, holds the words the check looks for. The
// first pull request to add this check had both lines in a fenced block showing what to write, and
// would have excused itself. So a fence is where prose stops being a statement, and the check reads
// past it.
func TestAnExampleIsNotTheLine(t *testing.T) {
	files := edited("internal/session/waiting.go")
	for _, one := range []struct {
		name string
		body string
		want []string
	}{
		{
			name: "the line inside a fence is an example",
			body: "**What.** The check reads the diff. The way out looks like this:\n\n```\nNo scenario: the behaviour is unchanged, this moves it between packages\n```\n",
			want: []string{Scenario},
		},
		{
			name: "a fence marked as a language is a fence too",
			body: "```text\nNo scenario: the behaviour is unchanged, this moves it between packages\n```\n",
			want: []string{Scenario},
		},
		{
			name: "an unclosed fence swallows the rest, which is what a reader sees too",
			body: "```\nNo scenario: the behaviour is unchanged, this moves it between packages\n",
			want: []string{Scenario},
		},
		{
			name: "the line outside the fence still counts",
			body: "```\nNo scenario: this one is an example\n```\n\nNo scenario: the behaviour is unchanged, this moves it between packages\n",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			got := missing(Check(Change{Files: files, Body: one.body}))
			if strings.Join(got, ",") != strings.Join(one.want, ",") {
				t.Fatalf("the check asks for %v, want %v", got, one.want)
			}
		})
	}
}

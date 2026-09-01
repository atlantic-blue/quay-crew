package contextspend_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/contextspend"
)

// A conversation nobody has spoken in has filled nothing, which is not the same as having filled it
// with nothing. A cell naming a category out of four zeroes reads as a measurement.
func TestNothingMeasuredSaysNothing(t *testing.T) {
	var nothing contextspend.Spend
	if !nothing.Empty() {
		t.Error("a spend with no characters in it does not say it is empty")
	}
	if cell := nothing.Cell(); cell != "" {
		t.Errorf("the cell reads %q for a conversation nobody has spoken in, want it blank", cell)
	}
	if lines := nothing.Lines(); lines != nil {
		t.Errorf("the breakdown reads %v for a conversation nobody has spoken in, want nothing", lines)
	}
	for _, category := range contextspend.Categories() {
		if share := nothing.Share(category); share != 0 {
			t.Errorf("%s takes %d per cent of nothing", category, share)
		}
	}
}

// The check against the model's own count is the thing that says whether the shares mean anything.
// Where the model has counted nothing there is no check to make, and a share worked out from a zero
// is a claim rather than a check.
func TestTheCheckRefusesToScoreWhatTheModelHasNotCounted(t *testing.T) {
	measured := contextspend.Spend{Reads: 40_000}
	for _, tc := range []struct {
		name    string
		check   contextspend.Check
		because string
	}{
		{
			name:    "the model has counted nothing",
			check:   measured.Against(0),
			because: "a conversation the model has not answered in has no count to check against",
		},
		{
			name:    "nothing was measured",
			check:   contextspend.Spend{}.Against(500_000),
			because: "there is no breakdown to hold against the count",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.check.Known() {
				t.Fatalf("the check claims it can be made\n\n%s", tc.because)
			}
			if share := tc.check.Share(); share != 0 {
				t.Errorf("the check scores %d per cent\n\n%s", share, tc.because)
			}
			if line := tc.check.Line(); strings.Contains(line, "%") {
				t.Errorf("the line makes a share up: %q\n\n%s", line, tc.because)
			}
		})
	}
}

// A command whose first word only looks like a reading command. The rule is the first word matched
// whole, because a prefix match puts every `catalogue` and every `sed -i` into the reads column and
// nobody would ever see that it had.
func TestACommandThatOnlyLooksLikeAReadIsNotOne(t *testing.T) {
	for _, tc := range []struct {
		command string
		because string
	}{
		{"", "there is no command at all"},
		{"   ", "there is no command at all"},
		{"catalogue --help", "the word is catalogue, and cat is only a prefix of it"},
		{"header --version", "the word is header, and head is only a prefix of it"},
		{"sed -i 's/a/b/' file.go", "sed writing a file back is not sed printing one"},
		{"grep -n needle file.go", "a search returns the lines that matched, not the file"},
		{"go test ./...", "the output is a test run"},
		{"echo cat file.go", "cat is an argument here, not the command"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			if contextspend.ReadsAFile(tc.command) {
				t.Errorf("%q is counted as reading a file\n\n%s", tc.command, tc.because)
			}
			if got := contextspend.Of(contextspend.Shell, tc.command); got != contextspend.Tools {
				t.Errorf("%q lands in %s, want %s\n\n%s", tc.command, got, contextspend.Tools, tc.because)
			}
		})
	}
}

// A tool this has never heard of returns tool output. Dropping it would make every other share larger
// than it is, and calling it a read would put the expensive thing and the cheap thing in one number.
func TestAToolThisDoesNotKnowIsToolOutput(t *testing.T) {
	for _, tool := range []string{"", "Grep", "Glob", "WebFetch", "SomethingBuiltNextYear"} {
		if got := contextspend.Of(tool, ""); got != contextspend.Tools {
			t.Errorf("what %q returned lands in %s, want %s", tool, got, contextspend.Tools)
		}
	}
}

// The contents of a file are a read however the session opened them. This crew's sessions are told to
// work through the shell, so without the shell rule the reads column reads two per cent when the
// measured answer is nearer thirty.
func TestAFileIsAReadHoweverItWasOpened(t *testing.T) {
	for _, tc := range []struct{ tool, command string }{
		{"Read", ""},
		{"NotebookRead", ""},
		{contextspend.Shell, "cat internal/job/controller.go"},
		{contextspend.Shell, "sed -n '1,50p' internal/job/controller.go"},
		{contextspend.Shell, "head -50 go.mod"},
		{contextspend.Shell, "tail -n 20 CHANGELOG.md"},
		{contextspend.Shell, "/bin/cat go.mod"},
		{contextspend.Shell, "cat go.mod | grep require"},
	} {
		t.Run(tc.tool+" "+tc.command, func(t *testing.T) {
			if got := contextspend.Of(tc.tool, tc.command); got != contextspend.Reads {
				t.Errorf("%s %q lands in %s, want %s", tc.tool, tc.command, got, contextspend.Reads)
			}
		})
	}
}

// The whole point of the four numbers: they add up. A breakdown whose parts do not sum to the number
// printed above them is a number that will be trusted and is wrong.
func TestThePartsSumToTheTotal(t *testing.T) {
	var spent contextspend.Spend
	spent.Count(contextspend.Reads, 4_000)
	spent.Count(contextspend.Tools, 3_000)
	spent.Count(contextspend.Turns, 2_000)
	spent.Count(contextspend.Told, 1_000)
	if got := spent.Total(); got != 10_000 {
		t.Fatalf("the total is %d, want the 10,000 the four parts add up to", got)
	}
	var summed int64
	for _, category := range contextspend.Categories() {
		summed += spent.Of(category)
	}
	if summed != spent.Total() {
		t.Errorf("the categories add up to %d and the total says %d", summed, spent.Total())
	}
}

// A category the caller invents is counted as told rather than dropped. The alternative is a total
// that silently loses characters, which is the shape of number this whole measurement exists to
// avoid.
func TestACategoryThisDoesNotKnowIsStillCounted(t *testing.T) {
	var spent contextspend.Spend
	spent.Count(contextspend.Category("attachments"), 500)
	if spent.Total() != 500 {
		t.Fatalf("the total is %d, want the 500 characters that were counted", spent.Total())
	}
	if spent.Told != 500 {
		t.Errorf("told holds %d of them, want all 500", spent.Told)
	}
}

func TestTheLargestCategoryIsTheAnswer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spend contextspend.Spend
		want  contextspend.Category
		cell  string
	}{
		{
			name:  "a session that read a lot of code",
			spend: contextspend.Spend{Reads: 600, Tools: 200, Turns: 150, Told: 50},
			want:  contextspend.Reads, cell: "reads 60%",
		},
		{
			name:  "a session that ran a lot of commands",
			spend: contextspend.Spend{Reads: 100, Tools: 700, Turns: 150, Told: 50},
			want:  contextspend.Tools, cell: "tools 70%",
		},
		{
			name:  "a session going round in circles",
			spend: contextspend.Spend{Reads: 100, Tools: 100, Turns: 750, Told: 50},
			want:  contextspend.Turns, cell: "turns 75%",
		},
		{
			name:  "a short conversation, mostly the task it was given",
			spend: contextspend.Spend{Told: 900, Turns: 100},
			want:  contextspend.Told, cell: "told 90%",
		},
		{
			// Two categories exactly level. The same conversation has to answer the same way twice,
			// or a listing shuffles between refreshes for no reason a reader can see.
			name:  "two categories level",
			spend: contextspend.Spend{Reads: 500, Tools: 500},
			want:  contextspend.Reads, cell: "reads 50%",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.spend.Largest(); got != tc.want {
				t.Errorf("the largest is %s, want %s", got, tc.want)
			}
			if got := tc.spend.Cell(); got != tc.cell {
				t.Errorf("the cell reads %q, want %q", got, tc.cell)
			}
			if again := tc.spend.Largest(); again != tc.spend.Largest() {
				t.Error("two readings of one spend name different categories")
			}
		})
	}
}

// The breakdown a person reads: every category, largest first, whatever order the fields happen to
// be in.
func TestTheBreakdownReadsLargestFirst(t *testing.T) {
	spent := contextspend.Spend{Reads: 100, Tools: 400, Turns: 300, Told: 200}
	lines := spent.Lines()
	if len(lines) != len(contextspend.Categories()) {
		t.Fatalf("the breakdown has %d lines, want one for each of the %d categories: %v",
			len(lines), len(contextspend.Categories()), lines)
	}
	for i, want := range []string{"tools", "turns", "told", "reads"} {
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("line %d reads %q, want it to start with %q", i, lines[i], want)
		}
	}
	if !strings.Contains(lines[0], "40%") || !strings.Contains(lines[0], "400 characters") {
		t.Errorf("the largest line reads %q, want the share and the count on it", lines[0])
	}
	if !strings.Contains(lines[0], contextspend.Tools.Meaning()) {
		t.Errorf("the largest line reads %q, want it to say what was counted", lines[0])
	}
}

// The comparison against the model's own count, which is what says the breakdown measured the same
// context the model charged for.
func TestTheCheckHoldsTheBreakdownAgainstTheModelsOwnCount(t *testing.T) {
	// 190,000 characters is 100,000 tokens at the measured ratio of 1.9.
	spent := contextspend.Spend{Reads: 190_000}
	if got := contextspend.Tokens(spent.Total()); got != 100_000 {
		t.Fatalf("190,000 characters comes to %d tokens, want 100,000 at the measured ratio", got)
	}

	check := spent.Against(125_000)
	if !check.Known() {
		t.Fatal("the check says it cannot be made, and both numbers are there")
	}
	if got := check.Share(); got != 80 {
		t.Errorf("the check scores %d per cent, want 80: 100,000 tokens of the 125,000 counted", got)
	}
	line := check.Line()
	for _, want := range []string{"100,000", "125,000", "80%", "system prompt"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line reads %q, want %q in it", line, want)
		}
	}

	// Over a hundred is a real answer and it is not hidden. It says the transcript holds more than the
	// model still carries, which is what a conversation the runtime compacted looks like. Capping it
	// would turn a fact worth seeing into a clean number.
	if got := spent.Against(50_000).Share(); got != 200 {
		t.Errorf("a compacted conversation scores %d per cent, want the 200 the arithmetic gives", got)
	}
}

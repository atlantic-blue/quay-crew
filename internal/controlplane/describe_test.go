package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
)

// A description costs a model call, and a crew running automation makes a thread per run, so when one
// is written matters as much as what it says. These cases are that decision written down.

func TestWhenAThreadIsWorthDescribingAgain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		turns     int
		lastAt    int
		every     int
		described bool
		because   string
	}{
		{
			name: "a thread nobody has spoken in", turns: 0, lastAt: 0, every: 10, described: false,
			because: "there is no conversation to describe, and a description of nothing is worse than none",
		},
		{
			name: "after the first turn", turns: 1, lastAt: 0, every: 10, described: true,
			because: "one turn is enough to say what a conversation is for, and it is what makes a listing readable at all",
		},
		{
			name: "the turn after that", turns: 2, lastAt: 1, every: 10, described: false,
			because: "a call per turn doubles the cost of a cheap turn, and two turns rarely change what a conversation is about",
		},
		{
			name: "the conversation has moved on", turns: 11, lastAt: 1, every: 10, described: true,
			because: "ten turns past the description is far enough that it is describing something else",
		},
		{
			name: "just short of moved on", turns: 10, lastAt: 1, every: 10, described: false,
			because: "the count is turns since, not turns total",
		},
		{
			name: "describing is off", turns: 5, lastAt: 0, every: 0, described: false,
			because: "a crew that turned it off pays for nothing, and off has to mean off on the first turn too",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := worthDescribing(tc.turns, tc.lastAt, tc.every); got != tc.described {
				t.Fatalf("with %d turns, last described at %d, every %d: %v, and %s",
					tc.turns, tc.lastAt, tc.every, got, tc.because)
			}
		})
	}
}

// The model is asked for one line and does not always give one. What it returns goes straight into a
// listing row, so a paragraph or a newline breaks the row rather than reading badly.
func TestADescriptionIsCutDownToSomethingARowCanHold(t *testing.T) {
	for _, tc := range []struct {
		name  string
		said  string
		want  string
		empty bool
	}{
		{name: "one line", said: "Blog post about the agentic harness", want: "Blog post about the agentic harness"},
		{name: "space around it", said: "  the electricity bill  ", want: "the electricity bill"},
		{
			name: "a paragraph", said: "Fixing the payout job.\n\nIt fails on the second retry.",
			want: "Fixing the payout job.",
		},
		{name: "nothing worth keeping", said: "   \n  ", empty: true},
		{
			name: "quoted, the way a model likes to answer",
			said: `"Blog post about the agentic harness"`, want: "Blog post about the agentic harness",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tidyDescription(tc.said)
			if tc.empty {
				if got != "" {
					t.Fatalf("tidyDescription(%q) = %q, want nothing", tc.said, got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("tidyDescription(%q) = %q, want %q", tc.said, got, tc.want)
			}
		})
	}
}

func TestALongDescriptionIsCapped(t *testing.T) {
	long := strings.Repeat("a", descriptionLimit*3)

	if got := tidyDescription(long); len([]rune(got)) != descriptionLimit {
		t.Fatalf("a description of %d characters was kept at %d, want %d",
			len(long), len([]rune(got)), descriptionLimit)
	}
}

// The setting is read from the crew's configuration, and an unreadable value must not quietly turn
// describing off or on.
func TestHowOftenAThreadIsDescribed(t *testing.T) {
	for _, tc := range []struct {
		configured string
		want       int
	}{
		{configured: "", want: describeEveryDefault},
		{configured: "10", want: 10},
		{configured: "3", want: 3},
		{configured: "off", want: 0},
		{configured: "0", want: 0},
		// Not a number and not "off". Keeping the default rather than refusing, because the crew
		// starting is worth more than this setting being exactly right.
		{configured: "sometimes", want: describeEveryDefault},
		{configured: "-4", want: describeEveryDefault},
	} {
		t.Run(tc.configured, func(t *testing.T) {
			if got := DescribeEvery(tc.configured); got != tc.want {
				t.Fatalf("describeEvery(%q) = %d, want %d", tc.configured, got, tc.want)
			}
		})
	}
}

// The cases above are the decision. This one is the feature: a thread that has had a turn says what
// it is about, without anybody naming it.
func TestAThreadDescribesItselfAfterItsFirstTurn(t *testing.T) {
	crew := describingCrewOf(t, 10)

	crew.dispatch(t, "help me write the blog post about the agentic harness")

	if got := crew.description(t); got != "blog post about the agentic harness" {
		t.Fatalf("the thread describes itself as %q", got)
	}
}

// A description is a convenience. A turn that worked is not reported as failed because the crew could
// not think of a name for it, and the operator is not left waiting for one either.
func TestATurnSucceedsEvenWhenDescribingFails(t *testing.T) {
	crew := describingCrewOf(t, 10)
	crew.runner.DescribeErr = errors.New("the model is not answering")

	reply := crew.dispatch(t, "help me write the blog post")

	if reply == "" {
		t.Fatal("the turn itself failed because describing did")
	}
	if got := crew.description(t); got != "" {
		t.Fatalf("a failed description was kept anyway: %q", got)
	}
}

// A crew that turned it off pays for nothing, which is the whole reason the setting exists: a flow
// makes a thread per run.
func TestACrewWithDescribingOffNeverAsks(t *testing.T) {
	crew := describingCrewOf(t, 0)

	crew.dispatch(t, "help me write the blog post")

	if crew.runner.Described != 0 {
		t.Fatalf("the crew asked for %d descriptions with describing off", crew.runner.Described)
	}
}

// Describing must not touch the operator's own name for a thread, which is the one thing in a listing
// that is certainly right.
func TestDescribingNeverOverwritesTheNameYouChose(t *testing.T) {
	crew := describingCrewOf(t, 10)
	crew.dispatch(t, "help me write the blog post about the agentic harness")

	if err := crew.store.SetLabel(context.Background(), crew.threadID, "the harness post"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	// Far enough past the description to be written again.
	for range 11 {
		crew.dispatch(t, "and another thing")
	}

	thread, err := crew.store.GetSession(context.Background(), crew.threadID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if thread.GetLabel() != "the harness post" {
		t.Fatalf("the name the operator chose became %q", thread.GetLabel())
	}
	if display.ThreadName(thread) != "the harness post" {
		t.Fatalf("the listing shows %q, want the operator's name", display.ThreadName(thread))
	}
}

// A backend that echoes hands the question back, and continuous integration runs one. Without this
// every thread is named "Here is the start of a conversation:", which is worse in a listing than the
// identifier it replaced.
func TestTheQuestionComingBackIsNotADescription(t *testing.T) {
	prompt := describePrompt([]*quaycrewv1.Turn{{Prompt: "write the blog post", Reply: "ok"}})

	for _, tc := range []struct {
		name string
		said string
		back bool
	}{
		{name: "the whole question echoed", said: prompt, back: true},
		{name: "the first line of it", said: "Here is the start of a conversation:", back: true},
		{name: "a line from the middle of it", said: "asked: write the blog post", back: true},
		{name: "an actual description", said: "blog post about the agentic harness", back: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTheQuestionBack(prompt, tidyDescription(tc.said)); got != tc.back {
				t.Fatalf("isTheQuestionBack(%q) = %v, want %v", tc.said, got, tc.back)
			}
		})
	}
}

// And end to end: a crew whose model echoes names nothing, rather than naming everything after the
// question.
func TestACrewWhoseModelEchoesNamesNothing(t *testing.T) {
	crew := describingCrewOf(t, 10)
	crew.runner.Echoes = true

	crew.dispatch(t, "help me write the blog post")

	if got := crew.description(t); got != "" {
		t.Fatalf("the thread is described as %q, which is the question it was asked", got)
	}
}

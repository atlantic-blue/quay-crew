package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
)

// A description costs a model call, and a system running automation makes a session per run, so when one
// is written matters as much as what it says. These cases are that decision written down.

func TestWhenASessionIsWorthDescribingAgain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tasks     int
		lastAt    int
		every     int
		described bool
		because   string
	}{
		{
			name: "a session nobody has spoken in", tasks: 0, lastAt: 0, every: 10, described: false,
			because: "there is no conversation to describe, and a description of nothing is worse than none",
		},
		{
			name: "after the first task", tasks: 1, lastAt: 0, every: 10, described: true,
			because: "one task is enough to say what a conversation is for, and it is what makes a listing readable at all",
		},
		{
			name: "the task after that", tasks: 2, lastAt: 1, every: 10, described: false,
			because: "a call per task doubles the cost of a cheap task, and two tasks rarely change what a conversation is about",
		},
		{
			name: "the conversation has moved on", tasks: 11, lastAt: 1, every: 10, described: true,
			because: "ten tasks past the description is far enough that it is describing something else",
		},
		{
			name: "just short of moved on", tasks: 10, lastAt: 1, every: 10, described: false,
			because: "the count is tasks since, not tasks total",
		},
		{
			name: "describing is off", tasks: 5, lastAt: 0, every: 0, described: false,
			because: "a system that turned it off pays for nothing, and off has to mean off on the first task too",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := worthDescribing(tc.tasks, tc.lastAt, tc.every); got != tc.described {
				t.Fatalf("with %d tasks, last described at %d, every %d: %v, and %s",
					tc.tasks, tc.lastAt, tc.every, got, tc.because)
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

// The setting is read from the system's configuration, and an unreadable value must not quietly task
// describing off or on.
func TestHowOftenASessionIsDescribed(t *testing.T) {
	for _, tc := range []struct {
		configured string
		want       int
	}{
		{configured: "", want: describeEveryDefault},
		{configured: "10", want: 10},
		{configured: "3", want: 3},
		{configured: "off", want: 0},
		{configured: "0", want: 0},
		// Not a number and not "off". Keeping the default rather than refusing, because the system
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

// The cases above are the decision. This one is the feature: a session that has had a task says what
// it is about, without anybody naming it.
func TestASessionDescribesItselfAfterItsFirstTask(t *testing.T) {
	system := describingSystemOf(t, 10)

	system.dispatch(t, "help me write the blog post about the agentic harness")

	if got := system.description(t); got != "blog post about the agentic harness" {
		t.Fatalf("the session describes itself as %q", got)
	}
}

// A description is a convenience. A task that worked is not reported as failed because the system could
// not think of a name for it, and the operator is not left waiting for one either.
func TestATaskSucceedsEvenWhenDescribingFails(t *testing.T) {
	system := describingSystemOf(t, 10)
	system.runner.DescribeErr = errors.New("the model is not answering")

	reply := system.dispatch(t, "help me write the blog post")

	if reply == "" {
		t.Fatal("the task itself failed because describing did")
	}
	if got := system.description(t); got != "" {
		t.Fatalf("a failed description was kept anyway: %q", got)
	}
}

// A system that turned it off pays for nothing, which is the whole reason the setting exists: a flow
// makes a session per run.
func TestASystemWithDescribingOffNeverAsks(t *testing.T) {
	system := describingSystemOf(t, 0)

	system.dispatch(t, "help me write the blog post")

	if system.runner.Described != 0 {
		t.Fatalf("the system asked for %d descriptions with describing off", system.runner.Described)
	}
}

// Describing must not touch the operator's own name for a session, which is the one thing in a listing
// that is certainly right.
func TestDescribingNeverOverwritesTheNameYouChose(t *testing.T) {
	system := describingSystemOf(t, 10)
	system.dispatch(t, "help me write the blog post about the agentic harness")

	if err := system.store.SetLabel(context.Background(), system.sessionID, "the harness post"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	// Far enough past the description to be written again.
	for range 11 {
		system.dispatch(t, "and another thing")
	}

	session, err := system.store.GetSession(context.Background(), system.sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.GetLabel() != "the harness post" {
		t.Fatalf("the name the operator chose became %q", session.GetLabel())
	}
	if display.SessionName(session) != "the harness post" {
		t.Fatalf("the listing shows %q, want the operator's name", display.SessionName(session))
	}
}

// A backend that echoes hands the question back, and continuous integration runs one. Without this
// every session is named "Here is the start of a conversation:", which is worse in a listing than the
// identifier it replaced.
func TestTheQuestionComingBackIsNotADescription(t *testing.T) {
	prompt := describePrompt([]*quaycrewv1.Task{{Prompt: "write the blog post", Reply: "ok"}})

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

// And end to end: a system whose model echoes names nothing, rather than naming everything after the
// question.
func TestASystemWhoseModelEchoesNamesNothing(t *testing.T) {
	system := describingSystemOf(t, 10)
	system.runner.Echoes = true

	system.dispatch(t, "help me write the blog post")

	if got := system.description(t); got != "" {
		t.Fatalf("the session is described as %q, which is the question it was asked", got)
	}
}

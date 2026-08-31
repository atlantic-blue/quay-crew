package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// What a session reads instead of being told. These drive the command through a real control plane
// and a real store, so what they hold is the answer a session actually gets.

func TestHistorySaysWhatRanWhatItCostAndWhatFailed(t *testing.T) {
	client := aSystemToJobIn(t)
	declaredHere(t, client, "read the electricity bill")
	declaredHere(t, client, "write the post")

	said := mustRun(t, client, "history")

	for _, want := range []string{"2 jobs", "read the electricity bill", "write the post"} {
		if !strings.Contains(said, want) {
			t.Errorf("krewe history does not say %q: %q", want, said)
		}
	}
}

// The window is named at the top, because a total is the first thing read and a reader who does not
// know which days it covers cannot use it for anything.
func TestHistoryNamesTheWindowItRead(t *testing.T) {
	client := aSystemToJobIn(t)
	declaredHere(t, client, "read the electricity bill")

	said := mustRun(t, client, "history", "--since", "2026-08-28", "--until", "2026-08-30")

	if !strings.Contains(said, "28 August 2026") || !strings.Contains(said, "31 August 2026") {
		t.Fatalf("krewe history does not name the window it read: %q", said)
	}
}

// A bare date at the far end means that whole day. Reading both ends as midnight silently drops the
// last day somebody named, which is the kind of wrong nobody goes back and checks.
func TestTheLastDayNamedIsIncludedWhole(t *testing.T) {
	client := aSystemToJobIn(t)
	declaredHere(t, client, "read the electricity bill")

	// The job was declared today, so a window whose far end is today only holds it if that day is
	// taken to its end.
	said := mustRun(t, client, "history", "--since", "2020-01-01", "--until", today())

	if !strings.Contains(said, "read the electricity bill") {
		t.Fatalf("a job declared today is outside a window ending today: %q", said)
	}
}

func TestADateAHistoryCannotReadIsRefusedByName(t *testing.T) {
	client := aSystemToJobIn(t)

	err := runOne(t, client, "history", "--since", "last tuesday")

	if err == nil {
		t.Fatal("a date the command cannot read was accepted")
	}
	// Named, and shown the spelling that works, because a refusal that does not say what to type
	// instead sends somebody to guess a second time.
	for _, want := range []string{"--since", "last tuesday", "2026-08-28"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}

func TestAWindowThatEndsBeforeItStartsIsRefusedToTheOperator(t *testing.T) {
	client := aSystemToJobIn(t)

	err := runOne(t, client, "history", "--since", "2026-08-30", "--until", "2026-08-28")

	if err == nil || !strings.Contains(err.Error(), "ends before it starts") {
		t.Fatalf("a backwards window was answered with %v", err)
	}
}

// The one that matters. A limit smaller than the window must not move the total, and the command has
// to say how many it did not print: a cap nobody is told about reads as complete coverage.
func TestTheTotalCoversTheWindowAndTheListingSaysWhatItLeftOut(t *testing.T) {
	client := aSystemToJobIn(t)
	for i := 0; i < 6; i++ {
		declaredHere(t, client, "a job")
	}

	said := mustRun(t, client, "history", "--limit", "2")

	if !strings.Contains(said, "6 jobs") {
		t.Fatalf("the total does not cover the window: %q", said)
	}
	if !strings.Contains(said, "4 jobs not shown") {
		t.Fatalf("the listing does not say what the limit left out: %q", said)
	}
}

// An empty window says so, and says how to widen it. "No jobs" with nothing after it reads as a
// broken crew rather than as a quiet week.
func TestAWindowWithNothingInItSaysHowToWidenIt(t *testing.T) {
	client := aSystemToJobIn(t)
	declaredHere(t, client, "read the electricity bill")

	said := mustRun(t, client, "history", "--since", "2020-01-01", "--until", "2020-01-02")

	if !strings.Contains(said, "no jobs were declared in that window") {
		t.Fatalf("an empty window says %q", said)
	}
	if !strings.Contains(said, "--since") {
		t.Fatalf("an empty window does not say how to widen it: %q", said)
	}
}

func TestHistoryTakesNoFlagItDoesNotUnderstand(t *testing.T) {
	client := aSystemToJobIn(t)

	err := runOne(t, client, "history", "--phase", "done")

	if err == nil || !strings.Contains(err.Error(), "--phase") {
		t.Fatalf("krewe history accepted a flag it does not take: %v", err)
	}
}

// A history that read one project must not be taken for the crew's, because the total above the rows
// looks identical either way.
func TestHistorySaysWhereItRead(t *testing.T) {
	client := aSystemToJobIn(t)
	declaredHere(t, client, "read the electricity bill")

	if said := mustRun(t, client, "history"); !strings.Contains(said, "me/house-bills") {
		t.Fatalf("a narrowed history does not name the address it read: %q", said)
	}
	if said := mustRun(t, client, "history", "system"); !strings.Contains(said, "every project") {
		t.Fatalf("a system wide history does not say it read every project: %q", said)
	}
}

// The manual is how a session finds a command without being told it exists, which is the whole point
// of this one.
func TestHistoryIsInTheManual(t *testing.T) {
	client := testClient(t)

	said := mustRun(t, client, "manual")

	if !strings.Contains(said, "history [<address>|system]") {
		t.Fatalf("the manual does not carry krewe history")
	}
	if !strings.Contains(said, "--since") {
		t.Fatalf("the manual does not say how to narrow a history")
	}
}

// runOne runs one invocation and hands back what it refused with, for the tests that are about a
// refusal rather than about output.
func runOne(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	return run(context.Background(), client, args, &out, "")
}

// today is the day the crew is having, written the way the command reads a date. A test that hard
// coded a date would start failing on its own the day after it was written.
func today() string { return time.Now().UTC().Format(time.DateOnly) }

// aWeekOfWork is a history with the shapes a reader actually meets: jobs that cost something, one
// that failed, one that was stopped, and one still going.
func aWeekOfWork() *quaycrewv1.GetHistoryResponse {
	on := func(day, hour int) *timestamppb.Timestamp {
		return timestamppb.New(time.Date(2026, time.August, day, hour, 4, 0, 0, time.UTC))
	}
	ran := func(day, hour int, minutes int) (*timestamppb.Timestamp, *timestamppb.Timestamp) {
		start := time.Date(2026, time.August, day, hour, 4, 0, 0, time.UTC)
		return timestamppb.New(start), timestamppb.New(start.Add(time.Duration(minutes) * time.Minute))
	}
	first, firstEnd := ran(29, 14, 18)
	second, secondEnd := ran(29, 9, 12)
	third, thirdEnd := ran(30, 11, 4)
	return &quaycrewv1.GetHistoryResponse{
		Since: on(24, 0), Until: on(31, 0),
		Total: &quaycrewv1.HistoryTotals{
			Jobs: 4, Done: 2, Failed: 1, Unfinished: 1,
			SpentTokens: 121_144, PullRequests: 2, Steers: 1, WorkingSeconds: 34 * 60,
		},
		Jobs: []*quaycrewv1.JobDigest{
			{Id: "3f2a1b9c0000", Title: "a failed job is continued rather than repeated",
				Role: "implementer", Phase: "done", SpentTokens: 62_140,
				PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/531",
				CreatedAt:   first, StartedAt: first, FinishedAt: firstEnd},
			{Id: "8c4d2e1a0000", Title: "a job counts the steers it took",
				Role: "implementer", Phase: "done", SpentTokens: 41_000, Steers: 1,
				PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/530",
				CreatedAt:   second, StartedAt: second, FinishedAt: secondEnd},
			{Id: "7c1d0e2f0000", Title: "prove the coverage gate ran",
				Role: "verifier", Phase: "failed", SpentTokens: 18_004,
				Reason:    "the gate was piped through tail, so its exit status said nothing",
				CreatedAt: third, StartedAt: third, FinishedAt: thirdEnd},
			{Id: "b2e5a7c40000", Title: "read the machine's headroom",
				Role: "implementer", Phase: "running", CreatedAt: on(30, 12)},
		},
	}
}

// What a reader is actually left with. A test that asserts on fields and never on the page can pass
// while the page is unreadable, which is how " tokens" survived a green suite once already.
func TestTheHistoryReadsAsAPage(t *testing.T) {
	var out strings.Builder
	writeHistory(&out, aWeekOfWork(), systemWide("jobs"))
	page := out.String()
	t.Log("\n" + page)

	for _, want := range []string{
		// The window and the total, above the rows, because a reader who only wants the week's cost
		// stops after three lines.
		"24 August 2026 to 31 August 2026",
		"4 jobs: 2 done, 1 failed, 1 still going",
		"121.1k tokens, 34m working",
		"2 pull requests, 1 steer",
		// A row: how long it took, what it cost, and what it was.
		"18m", "62.1k", "a failed job is continued rather than repeated",
		// And why the failure failed, under it.
		"piped through tail",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not say %q:\n%s", want, page)
		}
	}
	// A phase that counts nothing is left out, or the one number that matters is harder to see.
	if strings.Contains(page, "0 stopped") {
		t.Errorf("the page counts a phase that did not happen:\n%s", page)
	}
}

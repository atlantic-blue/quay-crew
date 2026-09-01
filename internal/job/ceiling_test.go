package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The gate, the record behind it, and what each of them refuses.
//
// The refusals come first in every case below, because a gate that only ever passes satisfies every
// test about passing. Three of them decide whether this behaviour helps or hurts: a window nobody has
// measured must not refuse work, a handoff that says nothing must not be written, and a session that
// wrote none must not have a fresh one started from it.

// TestAWindowNobodyMeasuredRefusesNothing. The size of a context window is not something the system
// can work out. It is what the model runtime last told a session in that workspace, and until one has
// been told there is no share to compare. A gate that read that silence as a full window would stop
// every job on a system nobody has told yet, which is worse than the failure it was built for.
func TestAWindowNobodyMeasuredRefusesNothing(t *testing.T) {
	for _, tc := range []struct {
		name       string
		used, size int64
		ceiling    int
		because    string
	}{
		{
			name: "nothing said how big the window is", used: 900_000, size: 0, ceiling: 70,
			because: "the size comes from the runtime, and a system nobody told holds no opinion",
		},
		{
			name: "a conversation nobody has spoken in", used: 0, size: 1_000_000, ceiling: 70,
			because: "an empty window is not a full one",
		},
		{
			name: "the workspace states no ceiling", used: 900_000, size: 1_000_000, ceiling: 0,
			because: "a zero ceiling is a caller that has not resolved it, and refusing every session on one would stop the system",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if job.PastTheCeiling(tc.used, tc.size, tc.ceiling) {
				t.Fatalf("%d of %d against a ceiling of %d refused work: %s",
					tc.used, tc.size, tc.ceiling, tc.because)
			}
		})
	}
}

// TestTheCeilingRefusesFromTheShareItPrints. The share is worked out where the console works it out,
// so the number a listing shows and the number the gate acts on are the same number. A session refused
// at 69 while its row reads 69 would read as the system refusing at random.
func TestTheCeilingRefusesFromTheShareItPrints(t *testing.T) {
	for _, tc := range []struct {
		name    string
		used    int64
		ceiling int
		want    bool
	}{
		{name: "well under it", used: 260_000, ceiling: 70, want: false},
		{name: "one point under it", used: 690_000, ceiling: 70, want: false},
		{name: "exactly at it", used: 700_000, ceiling: 70, want: true},
		{name: "past it", used: 820_000, ceiling: 70, want: true},
		{name: "a workspace that raised it", used: 820_000, ceiling: 90, want: false},
		{name: "a workspace that turned the gate off", used: 990_000, ceiling: 100, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := job.PastTheCeiling(tc.used, 1_000_000, tc.ceiling); got != tc.want {
				t.Fatalf("%d of 1,000,000 is %d per cent against a ceiling of %d: refused=%v, want %v",
					tc.used, job.ShareOf(tc.used, 1_000_000), tc.ceiling, got, tc.want)
			}
		})
	}
}

// TestAWorkspaceThatSaysNothingTakesTheSystemsCeiling, and it is a number rather than off. Every other
// limit on that row ships unset because none has been measured; this one ships set from the standard,
// which is the decision quay-crew#539 records.
func TestAWorkspaceThatSaysNothingTakesTheSystemsCeiling(t *testing.T) {
	if got := (job.Limits{}).ContextCeiling(); got != job.DefaultContextCeiling {
		t.Fatalf("a workspace that says nothing has a ceiling of %d, want the system's own %d",
			got, job.DefaultContextCeiling)
	}
	if got := (job.Limits{ContextCeilingPercent: 90}).ContextCeiling(); got != 90 {
		t.Fatalf("a workspace that set 90 has a ceiling of %d", got)
	}
}

// TestAHandoffThatSaysNothingIsRefused. The whole content of a handoff is what is left, and a fresh
// session given an empty one starts from nothing: it pays for every discovery the session before it
// made, which is more than the session at the ceiling would have cost.
func TestAHandoffThatSaysNothingIsRefused(t *testing.T) {
	for _, said := range []string{"", "   ", "\n\t "} {
		if _, _, err := job.TidyHandoff(said, "the third rebase"); err == nil {
			t.Fatalf("a handoff saying %q was written", said)
		} else if !strings.Contains(err.Error(), "what is left") {
			t.Fatalf("the refusal says %q, want it to say what is missing", err)
		}
	}
}

// TestAHandoffTooLongToReadIsRefused. It goes in front of the next session beside the brief, and a
// handoff nobody reads to the end is the failure the brief's own ceiling exists to stop.
func TestAHandoffTooLongToReadIsRefused(t *testing.T) {
	long := strings.Repeat("a", job.HandoffLimit+1)
	if _, _, err := job.TidyHandoff(long, ""); err == nil {
		t.Fatal("a handoff over the ceiling was written")
	}
	if _, _, err := job.TidyHandoff("finish the migration", long); err == nil {
		t.Fatal("what was tried was over the ceiling and was written")
	}
}

// TestASessionThatTriedNothingSaysNothing. Empty is a real answer here and not a missing one: a
// session that hit no dead end has none to report, and refusing it would have a model invent one.
func TestASessionThatTriedNothingSaysNothing(t *testing.T) {
	left, tried, err := job.TidyHandoff("  finish the migration and push  ", "  ")
	if err != nil {
		t.Fatalf("TidyHandoff: %v", err)
	}
	if left != "finish the migration and push" || tried != "" {
		t.Fatalf("the handoff reads %q / %q", left, tried)
	}
}

// TestTheFreshSessionIsToldWhatWasLeft, which is the whole point of the record. A test that a second
// session starts passes whether or not the handoff has anything in it, so this reads what the second
// session is actually handed.
func TestTheFreshSessionIsToldWhatWasLeft(t *testing.T) {
	one := &job.Job{
		ID: "0123456789abcdef01234567", Brief: "move the picks query onto the new index",
		Repository: "atlantic-blue/quay-crew", PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/561",
		Steps: []job.Step{{Seq: 1, Summary: "read the issue"}},
		Handoffs: []job.Handoff{{
			Seq: 1, Session: "aaaaaaaaaaaaaaaaaaaaaaaa",
			Left:  "the index is written, the query still reads the old one: branch 539-feat-index",
			Tried: "adding the index inside the migration that renames the column, which deadlocks",
		}},
	}
	handed := job.HandedOn(one)
	for _, want := range []string{
		"the query still reads the old one",
		"539-feat-index",
		"which deadlocks",
		"read the issue",
		"move the picks query onto the new index",
		"https://github.com/atlantic-blue/quay-crew/pull/561",
		"clone atlantic-blue/quay-crew",
	} {
		if !strings.Contains(handed, want) {
			t.Fatalf("the fresh session is not told %q:\n%s", want, handed)
		}
	}
}

// TestASessionThatTriedNothingIsSaidSoOutLoud. A heading with nothing under it reads as a record that
// got lost, and the next session then wonders what it is not being told.
func TestASessionThatTriedNothingIsSaidSoOutLoud(t *testing.T) {
	one := &job.Job{
		ID: "0123456789abcdef01234567", Brief: "read the electricity bill",
		Handoffs: []job.Handoff{{Seq: 1, Session: "aaaaaaaaaaaaaaaaaaaaaaaa", Left: "say when it is due"}},
	}
	if handed := job.HandedOn(one); !strings.Contains(handed, "recorded nothing it tried") {
		t.Fatalf("the fresh session is not told that nothing was tried:\n%s", handed)
	}
}

// TestAJobBeingHandedOverIsToldSoRatherThanAskedAgain. Asked is what every task of a job is built
// from, so the branch that carries a handoff has to sit in it: a fresh conversation given the brief
// alone would do the whole job a second time.
func TestAJobBeingHandedOverIsToldSoRatherThanAskedAgain(t *testing.T) {
	one := &job.Job{
		ID: "0123456789abcdef01234567", Brief: "read the electricity bill", Session: "",
		Handoffs: []job.Handoff{{Seq: 1, Session: "aaaaaaaaaaaaaaaaaaaaaaaa", Left: "say when it is due"}},
	}
	if !job.HandingOver(one) {
		t.Fatal("a job whose session was cleared by a handoff is not handing over")
	}
	if asked := job.Asked(one); !strings.Contains(asked, "say when it is due") {
		t.Fatalf("the task does not carry the handoff:\n%s", asked)
	}
	// Taken up. The conversation doing the job is the one the newest handoff names, so there is nothing
	// waiting to be handed to anybody.
	one.Session = "aaaaaaaaaaaaaaaaaaaaaaaa"
	if job.HandingOver(one) {
		t.Fatal("the session that wrote the handoff is being handed its own words")
	}
}

// TestAContinuedJobIsContinuedRatherThanHandedOver. A job that failed after it handed over goes back
// into the conversation that did the work, because the work is in it. Reading the handoff there would
// hand a session its own state as though it had never seen it.
func TestAContinuedJobIsContinuedRatherThanHandedOver(t *testing.T) {
	one := &job.Job{
		ID: "0123456789abcdef01234567", Brief: "read the electricity bill",
		Session: "bbbbbbbbbbbbbbbbbbbbbbbb", Resuming: "the runtime went away",
		Handoffs: []job.Handoff{{Seq: 1, Session: "aaaaaaaaaaaaaaaaaaaaaaaa", Left: "say when it is due"}},
	}
	if asked := job.Asked(one); !strings.Contains(asked, "the runtime went away") {
		t.Fatalf("a continued job was handed over instead of continued:\n%s", asked)
	}
}

// TestTheHandoffAskIsRecognisedByTheSystemThatSentIt. The controller reads the last task to know what
// came back. An ask it stopped recognising would be sent on every tick, and every ask is a task
// somebody pays for.
func TestTheHandoffAskIsRecognisedByTheSystemThatSentIt(t *testing.T) {
	one := &job.Job{ID: "0123456789abcdef01234567", Repository: "atlantic-blue/quay-crew"}
	asked := job.AskedForAHandoff(one, 70)
	if !job.AskingForAHandoff(asked) {
		t.Fatalf("the system does not recognise its own ask:\n%s", asked)
	}
	for _, want := range []string{"70 per cent", "krewe job handoff", "push", "atlantic-blue/quay-crew"} {
		if !strings.Contains(asked, want) {
			t.Fatalf("the ask does not say %q:\n%s", want, asked)
		}
	}
	if job.AskingForAHandoff(job.Asked(&job.Job{Brief: "read the electricity bill"})) {
		t.Fatal("an ordinary brief reads as the handoff ask")
	}
}

// TestEachHandoffNamesADifferentConversation. The point of handing over is a window that is empty, so
// the same handle would be the same full conversation.
func TestEachHandoffNamesADifferentConversation(t *testing.T) {
	id := "0123456789abcdef01234567"
	first, second, third := job.SessionAfter(id, 0), job.SessionAfter(id, 1), job.SessionAfter(id, 2)
	if first != job.SessionFor(id) {
		t.Fatalf("a job that never handed over runs in %q, want the session named after it", first)
	}
	if first == second || second == third {
		t.Fatalf("two attempts share a conversation: %q, %q, %q", first, second, third)
	}
}

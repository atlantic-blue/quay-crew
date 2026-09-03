package console

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/display"
)

// The key a person with a running system presses to see the conversations under a job.
//
// Enter on a job reads the job, so the sessions running under it are on a key of their own. A console
// that holds a system takes enter for the record, and a person watching a fan out still has to reach
// the six lines.

// TestTheSessionsKeyOpensTheSessionsRunningUnderTheJob presses it on a console that holds a system,
// which is the console an operator runs, and reads the lines off the screen it lands on.
func TestTheSessionsKeyOpensTheSessionsRunningUnderTheJob(t *testing.T) {
	client := aJobWhoseTestStageFannedOut()
	model := theJobsListing(t, client).WithClient(client)

	opened := walk(t, model, runes("s"))

	if opened.Reported() != nil {
		t.Fatalf("the sessions key was refused: %v", opened.Reported())
	}
	if opened.active.Name != "jobsessions" {
		t.Fatalf("the sessions key opened %q, want the conversations running under the job", opened.active.Name)
	}
	for _, session := range everySessionUnderTheJob() {
		if linesNaming(opened, session) != 1 {
			t.Fatalf("session %s is on %d lines of the screen the key opened, want one:\n%s",
				display.ShortID(session), linesNaming(opened, session), opened.View())
		}
	}
}

// And the record is still what enter reads on that same console, because the key that costs nothing
// belongs to the thing a person does most. A sessions key that took enter with it would move the
// reading a person opens a job for.
func TestEnterStillReadsTheJobOnAConsoleThatHoldsASystem(t *testing.T) {
	client := aJobWhoseTestStageFannedOut()
	model := theJobsListing(t, client).WithClient(client)

	opened := openTheJob(t, model)

	if !strings.Contains(drawnText(opened), "build the console view of one job") {
		t.Fatalf("enter no longer reads the job: the screen says\n%s", opened.View())
	}
}

// What the job did is still one conversation: the one the job runs in. Enter goes down into every
// conversation under the job now, so the key for the work has to name the one it wants or it lands on
// a listing of tasks nobody ran.
func TestTheWorkKeyStillOpensWhatTheJobsOwnConversationDid(t *testing.T) {
	client := aJobWhoseTestStageFannedOut()
	model := theJobsListing(t, client).WithClient(client)

	opened := walk(t, model, runes("w"))

	if opened.Reported() != nil {
		t.Fatalf("the work key was refused: %v", opened.Reported())
	}
	if opened.active.Name != "exec" {
		t.Fatalf("the work key opened %q, want what the job's conversation did", opened.active.Name)
	}
	if opened.parent != theSessionOfTheJob {
		t.Fatalf("the work is scoped to %q, want the conversation the job runs in", opened.parent)
	}
}

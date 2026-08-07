package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func at(minutes int) *timestamppb.Timestamp {
	return timestamppb.New(time.Date(2026, 8, 7, 12, minutes, 0, 0, time.UTC))
}

// TestThePanelOpensTheConversationYouWereLastIn. With nothing named, the right half is the thread the
// operator was last talking to, which is the one they meant.
func TestThePanelOpensTheConversationYouWereLastIn(t *testing.T) {
	got, found := newestSession([]*quaycrewv1.Session{
		{Id: "older", ModelSessionId: "c1", UpdatedAt: at(10)},
		{Id: "newest", ModelSessionId: "c2", UpdatedAt: at(40)},
		{Id: "middle", ModelSessionId: "c3", UpdatedAt: at(25)},
	})
	if !found {
		t.Fatal("no conversation was chosen from three")
	}
	if got != "newest" {
		t.Fatalf("the panel opens %q, want the one last spoken to", got)
	}
}

// TestThePanelSkipsWhatCannotBeOpened. A session with no conversation behind it refuses to attach, so
// choosing one would build a panel whose right half dies the moment it opens.
func TestThePanelSkipsWhatCannotBeOpened(t *testing.T) {
	got, found := newestSession([]*quaycrewv1.Session{
		{Id: "has-a-conversation", ModelSessionId: "c1", UpdatedAt: at(10)},
		{Id: "never-had-a-turn", UpdatedAt: at(40)},
		{Id: "put-away", ModelSessionId: "c2", UpdatedAt: at(50), ArchivedAt: at(50)},
	})
	if !found {
		t.Fatal("nothing was chosen, though one session can be opened")
	}
	if got != "has-a-conversation" {
		t.Fatalf("the panel opens %q, which cannot be attached to", got)
	}
}

// TestThePanelRefusesRatherThanOpeningHalfOfOne, and says what to type. A pane whose command exits is
// closed by tmux, so the layout would collapse to one pane and read as the panel being broken.
func TestThePanelRefusesRatherThanOpeningHalfOfOne(t *testing.T) {
	if _, found := newestSession(nil); found {
		t.Fatal("a conversation was chosen from an empty crew")
	}
	if _, found := newestSession([]*quaycrewv1.Session{{Id: "never-had-a-turn", UpdatedAt: at(10)}}); found {
		t.Fatal("a session that has never had a turn was offered as something to open")
	}
}

// TestWhereNamesTheAddressInTheRefusal, so the refusal says which crew it looked in rather than
// leaving the operator to guess whether they are standing somewhere unexpected.
func TestWhereNamesTheAddressInTheRefusal(t *testing.T) {
	for _, test := range []struct {
		name              string
		workspace, projct string
		want              string
	}{
		{"standing nowhere", "", "", ""},
		{"in a workspace", "juliantellez", "", " in juliantellez"},
		{"in a project", "juliantellez", "quay-crew", " in juliantellez/quay-crew"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := where(workspace.Path{Workspace: test.workspace, Project: test.projct})
			if got != test.want {
				t.Fatalf("where = %q, want %q", got, test.want)
			}
		})
	}
}

// TestQuayOpensTheCrewNotJustTheConsole. One command: `quay` opens the header, the console and a
// conversation. Nothing tested this until a mutation that made `quay` open the console alone stayed
// green, which is the whole of what was asked for going unwatched.
func TestQuayOpensTheCrewNotJustTheConsole(t *testing.T) {
	panelRan, aloneRan := false, false
	err := openTheCrew(
		func() error { panelRan = true; return nil },
		func() error { aloneRan = true; return nil },
	)
	if err != nil {
		t.Fatalf("openTheCrew: %v", err)
	}
	if !panelRan {
		t.Fatal("quay did not open the crew")
	}
	if aloneRan {
		t.Fatal("quay opened the console as well as the crew")
	}
}

// TestQuayOpensTheConsoleWhenThereIsNothingToPutBesideIt: a crew with no conversation in it is the
// first run, and refusing to open at all then would be absurd.
func TestQuayOpensTheConsoleWhenThereIsNothingToPutBesideIt(t *testing.T) {
	aloneRan := false
	err := openTheCrew(
		func() error { return fmt.Errorf("%w: no conversation yet", errNothingBeside) },
		func() error { aloneRan = true; return nil },
	)
	if err != nil {
		t.Fatalf("openTheCrew: %v", err)
	}
	if !aloneRan {
		t.Fatal("quay refused to open at all with nothing to put beside the console")
	}
}

// TestThePanelCommandIsRefused is rule 46 in this repository: when a command is removed, test the way
// off it. `quay panel` is in somebody's fingers and in these notes, so it has to fail loudly and name
// what to type instead rather than being taken for an unknown word.
func TestThePanelCommandIsRefused(t *testing.T) {
	err := run(context.Background(), testClient(t), []string{"panel"}, io.Discard, "")
	if err == nil {
		t.Fatal("quay panel was accepted")
	}
	if !strings.Contains(err.Error(), "`quay` on its own") {
		t.Fatalf("the refusal is %q, want it to name what to type instead", err)
	}
	// And it is gone from the usage, or it reads as still being a command.
	if strings.Contains(usage, "panel [<session id>]") {
		t.Fatal("the usage still lists a panel command")
	}
}

// TestQuaySaysWhyTheCrewCouldNotOpen. Every failure to open the panel used to come out as a single
// console pane: tmux missing, a crew with two projects and nowhere named to open, a header with no
// room to draw in. All of them looked the same from the outside, so the panel read as a thing that
// sometimes does not appear, and the reason was never printed anywhere.
func TestQuaySaysWhyTheCrewCouldNotOpen(t *testing.T) {
	aloneRan := false
	err := openTheCrew(
		func() error {
			return fmt.Errorf("panel: new-session: exec: \"tmux\": executable file not found in $PATH")
		},
		func() error { aloneRan = true; return nil },
	)
	if err == nil {
		t.Fatal("opening the crew failed and quay said nothing about it")
	}
	if !strings.Contains(err.Error(), "tmux") {
		t.Fatalf("the failure is reported as %q, and it does not say what went wrong", err)
	}
	if aloneRan {
		t.Fatal("quay opened a single console pane over a failure that was not about having nothing to put beside it")
	}
}

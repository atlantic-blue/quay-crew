package main

import (
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

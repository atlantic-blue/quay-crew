package console

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The view that lists what a session ran is called exec. It was called tasks, and the way off a word
// is the half of a rename that gets skipped: every spelling that opened it then opens it now.
func TestTheExecViewAnswersToItsWordAndToTheOnesItReplaced(t *testing.T) {
	registry, err := NewDefaultRegistry(&jobClient{})
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for _, typed := range []string{"exec", "e", "tasks", "task", "history", "h"} {
		resource, found := registry.Resolve(typed)
		if !found {
			t.Fatalf("typing %q opens nothing", typed)
		}
		if resource.Name != "exec" {
			t.Fatalf("typing %q opens %q, want exec", typed, resource.Name)
		}
	}
}

// The whole way in, not the lookup: the word is typed into the command bar the way an operator types
// it, and what is asserted is the view they are left on.
func TestTheWordTheViewUsedToBeCalledStillOpensIt(t *testing.T) {
	client := &jobClient{tasks: []*quaycrewv1.Task{{
		Id: "4444444444444444dddddddd", Session: "2222222222222222bbbbbbbb",
		Status: "done", Prompt: "read the electricity bill", Reply: "it is due on the 14th",
		OccurredAt: timestamppb.New(time.Now()),
	}}}
	model := newTestModel(t, Jobs(client), Exec(client))
	model.parent = "2222222222222222bbbbbbbb"

	model, _ = update(t, model, runes(":"))
	model = typeAll(t, model, "tasks")
	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.active.Name != "exec" {
		t.Fatalf("typing tasks left the operator on %q, want the exec view", model.active.Name)
	}
	if cmd == nil {
		t.Fatal("the word opened no listing, so the view would draw empty")
	}
	model, _ = update(t, model, cmd())
	// The breadcrumb is what tells the operator where the word took them, and it says the name the
	// view has now rather than the one they typed.
	if !strings.Contains(model.View(), "exec") {
		t.Fatalf("the screen does not say which view this is:\n%s", model.View())
	}
}

// The word is the only thing that moved. What the view lists, and the two keys that reach the machine
// from it, are what they were.
func TestTheExecViewStillListsWhatASessionRan(t *testing.T) {
	client := &jobClient{tasks: []*quaycrewv1.Task{{
		Id: "4444444444444444dddddddd", Session: "2222222222222222bbbbbbbb",
		Status: "done", Prompt: "read the electricity bill", Reply: "it is due on the 14th",
		OccurredAt: timestamppb.New(time.Now()),
	}}}

	rows, err := Exec(client).List(context.Background(), "2222222222222222bbbbbbbb")
	if err != nil {
		t.Fatalf("listing what the session ran: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the view lists %d rows, want the one task the session ran", len(rows))
	}
	if client.listedFrom != "2222222222222222bbbbbbbb" {
		t.Fatalf("the tasks were read from %q, want the session the view is scoped to", client.listedFrom)
	}
	if _, opens := actionNamed(Exec(client), "Open"); !opens {
		t.Fatal("the exec view no longer opens the conversation")
	}
	if _, shells := actionNamed(Exec(client), "Shell"); !shells {
		t.Fatal("the exec view no longer opens a shell in the sandbox")
	}
	// Opened on its own it has no session, and it says so rather than listing nothing.
	if _, err := Exec(client).List(context.Background(), ""); err == nil {
		t.Fatal("the view listed with no session, so an operator sees an empty screen and no reason for it")
	}
}

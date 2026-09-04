package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/model"
)

// A caller that already knows what the session is for says so at dispatch, so the name is on the row
// from the first second rather than after a model writes a description behind an exec that has landed.

func TestADispatchNamesTheSessionItMakes(t *testing.T) {
	s := newServer(&model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	_, project := newProject(t, s)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the electricity bill", Title: "read the electricity bill",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	read, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if read.GetSession().GetTitle() != "read the electricity bill" {
		t.Fatalf("the session was dispatched with a title and is called %q", read.GetSession().GetTitle())
	}
	// The title is its own field. Writing it into the label would spend the one name the operator
	// owns, and the next dispatch would then be writing over their word.
	if label := read.GetSession().GetLabel(); label != "" {
		t.Fatalf("the dispatch wrote %q into the operator's own label", label)
	}
}

// The failure this design is shaped around. A job's controller dispatches again after its controller
// died, and the operator may have renamed the conversation in between.
func TestADispatchMadeAgainDoesNotRenameTheConversation(t *testing.T) {
	s := newServer(&model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	_, project := newProject(t, s)

	first, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the electricity bill", Title: "read the electricity bill",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := s.SetSessionLabel(ctx, &quaycrewv1.SetSessionLabelRequest{
		Id: first.GetId(), Label: "the bill that is actually overdue",
	}); err != nil {
		t.Fatalf("SetSessionLabel: %v", err)
	}

	if _, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Handle: first.GetHandle(), Text: "read it again", Title: "something else entirely",
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	read, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: first.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got := read.GetSession().GetLabel(); got != "the bill that is actually overdue" {
		t.Fatalf("the operator's name for the conversation is now %q", got)
	}
	if got := read.GetSession().GetTitle(); got != "read the electricity bill" {
		t.Fatalf("the title moved to %q, and it is read only when the session is made", got)
	}
}

// A title goes into a listing row, so it is held to what a row can hold, the way a label is.
func TestATitleTooLongForARowIsCappedRatherThanRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	_, project := newProject(t, s)

	long := strings.Repeat("a", 200)
	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the electricity bill", Title: "  " + long + "\nand more  ",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	read, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	title := read.GetSession().GetTitle()
	if strings.ContainsAny(title, "\r\n") {
		t.Fatalf("the title carries a newline, which draws a row two rows tall: %q", title)
	}
	if len([]rune(title)) != 60 {
		t.Fatalf("a title of %d characters was kept at %d", len(long), len([]rune(title)))
	}
}

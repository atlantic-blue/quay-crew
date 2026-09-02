package main

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// Which of the four stages a job is in, on the two surfaces a person reads a job on. The phase says
// what the system is doing with the row and the stage says how far through the work it is, so both
// are printed and neither replaces the other.

func TestJobShowSaysTheStageAndWhatClosesAndOpensIt(t *testing.T) {
	client, _, id := aJobWaitingToBeAnswered(t)

	shown := mustRun(t, client, "job", "show", id)

	for _, want := range []string{
		"stage 1 of 4: ideation",
		"phase asking",
		"nothing came before it, ideation is the first stage",
		"design opens on your answer to what it understood",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show does not say %q: %q", want, shown)
		}
	}
	// A job in the stage that is built is told nothing about a stage that is not.
	if strings.Contains(shown, "is not built yet") {
		t.Errorf("a job in ideation is told a stage is not built: %q", shown)
	}
}

// The stage after the answer, and the truth about it: design is a later slice, so the job carries on
// under the plan a person approved rather than doing anything a design stage would do.
func TestJobShowSaysWhenTheStageItIsInIsNotBuilt(t *testing.T) {
	client, _, id := aJobWaitingToBeAnswered(t)

	mustRun(t, client, "job", "answer", id, "the surface is the command line, nothing else changes")

	shown := mustRun(t, client, "job", "show", id)
	for _, want := range []string{
		"stage 2 of 4: design",
		"ideation closed on your answer to what it understood",
		"nothing opens test yet, it is a later slice",
		"design is not built yet, so this job carries on under the plan a person approved",
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show does not say %q: %q", want, shown)
		}
	}
}

// A job stuck in ideation reads differently from one that is further on, which is the whole reason
// the column is in the listing.
func TestJobListCarriesTheStage(t *testing.T) {
	client, _, id := aJobWaitingToBeAnswered(t)

	waiting := lineFor(t, mustRun(t, client, "job", "list"), id)
	if !strings.Contains(waiting, "ideation") {
		t.Fatalf("the listing does not say a job waiting for its answer is in ideation: %q", waiting)
	}

	mustRun(t, client, "job", "answer", id, "the surface is the command line")

	moved := lineFor(t, mustRun(t, client, "job", "list"), id)
	if !strings.Contains(moved, "design") {
		t.Fatalf("the listing does not move the job on: %q", moved)
	}
	if strings.Contains(moved, "ideation") {
		t.Fatalf("the listing still reads the job as in ideation: %q", moved)
	}
	// The phase is still its own column. The stage sits beside it rather than in place of it.
	if !strings.Contains(waiting, "asking") {
		t.Fatalf("the listing dropped the phase when it gained the stage: %q", waiting)
	}
}

// A job that states no sentence never enters the stages, and both surfaces say so rather than
// naming a stage it is not in.
func TestAnErrandReadsAsHavingNoStage(t *testing.T) {
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "rotate the signing key",
		"--brief", "rotate the key and say what changed")

	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(listed.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs (%v), want the one just declared", len(listed.GetJobs()), err)
	}
	id := listed.GetJobs()[0].GetId()

	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "no stage") || !strings.Contains(shown, "errand") {
		t.Fatalf("krewe job show names a stage for a job that states no sentence: %q", shown)
	}
	for _, stage := range []string{"ideation", "design"} {
		if strings.Contains(shown, stage) {
			t.Fatalf("krewe job show puts an errand in %s: %q", stage, shown)
		}
	}
	if line := lineFor(t, mustRun(t, client, "job", "list"), id); !strings.Contains(line, " - ") {
		t.Fatalf("the listing carries no dash for a job with no stage: %q", line)
	}
}

// lineFor is the row a listing carries for one job, found by the short identifier the listing
// prints.
func lineFor(t *testing.T, listing, id string) string {
	t.Helper()
	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, short) {
			return line
		}
	}
	t.Fatalf("the listing carries no row for %s: %q", short, listing)
	return ""
}

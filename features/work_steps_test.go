package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/work"
	"github.com/cucumber/godog"
)

// Work is declared intent the crew keeps. These steps drive the control plane over its real
// interface, the way the command line does, and read the record back the same way.
//
// Nothing runs the work. What is specified here is the record, the refusals, and that the intent
// outlives the caller that declared it.

type workKey struct{}

// workWorld is what one scenario declared.
type workWorld struct {
	// declared holds each piece of work the scenario made, oldest first.
	declared []*quaycrewv1.Work
	listed   []*quaycrewv1.Work
}

func workFrom(ctx context.Context) *workWorld {
	w, _ := ctx.Value(workKey{}).(*workWorld)
	return w
}

func initializeWorkSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, workKey{}, &workWorld{}), nil
	})

	sc.Step(`^a piece of work titled "([^"]*)"$`, func(ctx context.Context, title string) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: title, Brief: "open the bill and say when it is due",
		})
	})

	// An answer is written by a controller, and no controller exists yet, so the record is put
	// there the way the controller will write it: on the row.
	sc.Step(`^a piece of work titled "([^"]*)" that answered "([^"]*)"$`,
		func(ctx context.Context, title, answer string) error {
			w, scenario := worldFrom(ctx), workFrom(ctx)
			declared := &work.Work{
				ID: store.NewID(), Workspace: w.workspaceID, Project: w.projectID,
				Title: title, Brief: "open the bill and say when it is due",
				Version: 1, Phase: work.PhaseDone, Answer: answer,
			}
			if err := w.store.CreateWork(ctx, declared, &work.Event{
				ID: store.NewID(), Kind: work.EventDeclared, Work: declared.ID,
				Workspace: w.workspaceID, Project: w.projectID, Detail: title,
				OccurredAt: time.Now().UTC(),
			}); err != nil {
				return err
			}
			found, err := w.client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: declared.ID})
			if err != nil {
				return err
			}
			scenario.declared = append(scenario.declared, found.GetWork())
			return nil
		})

	sc.Step(`^the caller declares work carrying an identifier of its own$`, func(ctx context.Context) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "read the electricity bill", Brief: "open it", Id: "0123456789abcdef01234567",
		})
	})

	sc.Step(`^the caller declares work carrying a parent$`, func(ctx context.Context) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "read the electricity bill", Brief: "open it", Parent: "0123456789abcdef01234567",
		})
	})

	sc.Step(`^the caller declares work with no title$`, func(ctx context.Context) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{Brief: "open it"})
	})

	sc.Step(`^the caller declares work with a title of (\d+) bytes$`, func(ctx context.Context, bytes int) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: strings.Repeat("t", bytes), Brief: "open it",
		})
	})

	sc.Step(`^the caller declares work with a brief of (\d+) bytes$`, func(ctx context.Context, bytes int) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "read the electricity bill", Brief: strings.Repeat("b", bytes),
		})
	})

	// A role has to be imported and attached before work can name it, which is the same two steps an
	// operator takes.
	sc.Step(`^the workspace holds the role "([^"]*)" at version (\d+)$`,
		func(ctx context.Context, name string, version int) error {
			w := worldFrom(ctx)
			if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
				Files: roleFiles(name, version, roleManifest{model: "opus", receives: []string{"work"}}),
			}); err != nil {
				return err
			}
			_, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Workspace: w.workspaceID, Name: name,
			})
			return err
		})

	sc.Step(`^the caller declares work in the role "([^"]*)"$`, func(ctx context.Context, role string) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "clear the backlog", Brief: "read the open pull requests", Role: role,
		})
	})

	sc.Step(`^the caller declares work in the mode "([^"]*)"$`, func(ctx context.Context, mode string) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "read the electricity bill", Brief: "open it", Mode: mode,
		})
	})

	sc.Step(`^the caller declares work expecting the file "([^"]*)"$`, func(ctx context.Context, path string) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "read the electricity bill", Brief: "open it", ExpectFile: path,
		})
	})

	sc.Step(`^the caller declares work after "([^"]*)"$`, func(ctx context.Context, id string) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "pay the electricity bill", Brief: "pay it", After: []string{id},
		})
	})

	sc.Step(`^the caller declares work after the first piece of work$`, func(ctx context.Context) error {
		first, err := firstWork(ctx)
		if err != nil {
			return err
		}
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "pay the electricity bill", Brief: "pay it", After: []string{first.GetId()},
		})
	})

	sc.Step(`^the caller declares work with a budget of (-?\d+) tokens$`, func(ctx context.Context, tokens int) error {
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "read the electricity bill", Brief: "open it", BudgetTokens: int64(tokens),
		})
	})

	sc.Step(`^the caller declares work carrying (\d+) labels$`, func(ctx context.Context, count int) error {
		labels := map[string]string{}
		for i := 0; i < count; i++ {
			labels[fmt.Sprintf("key-%d", i)] = "value"
		}
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "read the electricity bill", Brief: "open it", Labels: labels,
		})
	})

	sc.Step(`^the caller declares work carrying a label value of (\d+) characters$`,
		func(ctx context.Context, size int) error {
			return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
				Title: "read the electricity bill", Brief: "open it",
				Labels: map[string]string{"owner": strings.Repeat("v", size)},
			})
		})

	sc.Step(`^the caller declares work in a project that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
			Project: "nowhere", Title: "read the electricity bill", Brief: "open it",
		})
		return nil
	})

	sc.Step(`^the caller asks for a piece of work that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: "0123456789abcdef01234567"})
		return nil
	})

	// The caller's own context is cancelled, which is what a closed terminal does to the call it
	// was holding. Whatever is read afterwards is read by somebody else entirely.
	sc.Step(`^the caller goes away and the crew is asked again$`, func(ctx context.Context) error {
		scenario := workFrom(ctx)
		if len(scenario.declared) == 0 {
			return fmt.Errorf("no work was declared, so there is nothing to come back to")
		}
		calling, hangUp := context.WithCancel(ctx)
		hangUp()
		w := worldFrom(ctx)
		if _, err := w.client.GetWork(calling, &quaycrewv1.GetWorkRequest{Id: scenario.declared[0].GetId()}); err == nil {
			return fmt.Errorf("the call the caller hung up on answered anyway, so the caller never went away")
		}
		found, err := w.client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: scenario.declared[0].GetId()})
		if err != nil {
			return err
		}
		scenario.declared[0] = found.GetWork()
		return nil
	})

	sc.Step(`^the work is still there, pending, with its brief whole$`, func(ctx context.Context) error {
		one, err := firstWork(ctx)
		if err != nil {
			return err
		}
		if one.GetPhase() != work.PhasePending {
			return fmt.Errorf("the work is %q, want pending", one.GetPhase())
		}
		if one.GetBrief() != "open the bill and say when it is due" {
			return fmt.Errorf("the brief reads back as %q", one.GetBrief())
		}
		return nil
	})

	sc.Step(`^the work is pending$`, func(ctx context.Context) error {
		one, err := firstWork(ctx)
		if err != nil {
			return err
		}
		if one.GetPhase() != work.PhasePending {
			return fmt.Errorf("the work is %q, want pending", one.GetPhase())
		}
		return nil
	})

	sc.Step(`^the work is at depth (\d+) with no parent$`, func(ctx context.Context, depth int) error {
		one, err := firstWork(ctx)
		if err != nil {
			return err
		}
		if one.GetDepth() != int32(depth) || one.GetParent() != "" {
			return fmt.Errorf("the work is at depth %d under %q", one.GetDepth(), one.GetParent())
		}
		return nil
	})

	sc.Step(`^the work carries the moment it was declared$`, func(ctx context.Context) error {
		one, err := firstWork(ctx)
		if err != nil {
			return err
		}
		if one.GetCreatedAt() == nil || one.GetCreatedAt().AsTime().IsZero() {
			return fmt.Errorf("the work does not say when it was declared")
		}
		return nil
	})

	sc.Step(`^the work carries the role at version (\d+)$`, func(ctx context.Context, version int) error {
		one, err := lastWork(ctx)
		if err != nil {
			return err
		}
		if one.GetRoleVersion() != int32(version) {
			return fmt.Errorf("the work runs as %s at version %d, want version %d",
				one.GetRole(), one.GetRoleVersion(), version)
		}
		return nil
	})

	sc.Step(`^the work waits for the first piece of work$`, func(ctx context.Context) error {
		scenario := workFrom(ctx)
		if len(scenario.declared) < 2 {
			return fmt.Errorf("%d pieces of work were declared, want two", len(scenario.declared))
		}
		waits := scenario.declared[1].GetAfter()
		if len(waits) != 1 || waits[0] != scenario.declared[0].GetId() {
			return fmt.Errorf("the work waits for %v, want the first piece of work", waits)
		}
		return nil
	})

	sc.Step(`^the crew refuses it and says it assigns the identifier$`, theRefusalSays("assigns the identifier"))
	sc.Step(`^the crew refuses it and says the parent comes from the credential$`, theRefusalSays("credential"))
	sc.Step(`^the crew refuses it and says a title is needed$`, theRefusalSays("title"))
	sc.Step(`^the crew refuses it and says the ceiling is (\d+)$`, func(ctx context.Context, ceiling int) error {
		return theRefusalSays(fmt.Sprintf("%d", ceiling))(ctx)
	})
	sc.Step(`^the crew refuses it and names the role$`, theRefusalSays("backlog-clearer"))
	sc.Step(`^the crew refuses it and lists the modes$`, func(ctx context.Context) error {
		for _, mode := range []string{"plan", "edits", "dangerous"} {
			if err := theRefusalSays(mode)(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the crew refuses it and says the path is read inside the working directory$`,
		theRefusalSays("working directory"))
	sc.Step(`^the crew refuses it and says the path climbs out$`, theRefusalSays("climbs out"))
	sc.Step(`^the crew refuses it and names the identifier it cannot find$`,
		theRefusalSays("0123456789abcdef01234567"))
	sc.Step(`^the crew refuses it and says a budget cannot be below zero$`, theRefusalSays("below zero"))
	sc.Step(`^the crew refuses it and says the work already ended$`, theRefusalSays("already"))

	sc.Step(`^the caller lists the work in the project$`, func(ctx context.Context) error {
		return listWork(ctx, &quaycrewv1.ListWorkRequest{Project: worldFrom(ctx).projectID})
	})

	sc.Step(`^the caller lists the work that is pending$`, func(ctx context.Context) error {
		return listWork(ctx, &quaycrewv1.ListWorkRequest{
			Project: worldFrom(ctx).projectID, Phase: work.PhasePending,
		})
	})

	sc.Step(`^the listing holds both pieces of work, newest first$`, func(ctx context.Context) error {
		scenario := workFrom(ctx)
		if len(scenario.listed) != 2 {
			return fmt.Errorf("the listing holds %d pieces of work, want 2", len(scenario.listed))
		}
		if scenario.listed[0].GetId() != scenario.declared[1].GetId() {
			return fmt.Errorf("the listing opens with %q, want the newest first", scenario.listed[0].GetTitle())
		}
		return nil
	})

	sc.Step(`^the listing holds only "([^"]*)"$`, func(ctx context.Context, title string) error {
		scenario := workFrom(ctx)
		if len(scenario.listed) != 1 {
			return fmt.Errorf("the listing holds %d pieces of work, want 1", len(scenario.listed))
		}
		if scenario.listed[0].GetTitle() != title {
			return fmt.Errorf("the listing holds %q, want %q", scenario.listed[0].GetTitle(), title)
		}
		return nil
	})

	sc.Step(`^the listing carries the title and not the answer$`, func(ctx context.Context) error {
		scenario := workFrom(ctx)
		if len(scenario.listed) != 1 {
			return fmt.Errorf("the listing holds %d pieces of work, want 1", len(scenario.listed))
		}
		if scenario.listed[0].GetAnswer() != "" {
			return fmt.Errorf("the listing carries an answer: %q", scenario.listed[0].GetAnswer())
		}
		if scenario.listed[0].GetTitle() == "" {
			return fmt.Errorf("the listing carries no title, and a title is what a listing is for")
		}
		return nil
	})

	sc.Step(`^reading that one piece of work carries the answer whole$`, func(ctx context.Context) error {
		one, err := firstWork(ctx)
		if err != nil {
			return err
		}
		found, err := worldFrom(ctx).client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: one.GetId()})
		if err != nil {
			return err
		}
		if found.GetWork().GetAnswer() != "the bill is due on the 14th" {
			return fmt.Errorf("the answer reads back as %q", found.GetWork().GetAnswer())
		}
		return nil
	})

	sc.Step(`^the caller stops the first piece of work saying "([^"]*)"$`, func(ctx context.Context, reason string) error {
		one, err := firstWork(ctx)
		if err != nil {
			return err
		}
		w := worldFrom(ctx)
		stopped, err := w.client.StopWork(ctx, &quaycrewv1.StopWorkRequest{Id: one.GetId(), Reason: reason})
		w.lastErr = err
		if err == nil {
			workFrom(ctx).declared[0] = stopped.GetWork()
		}
		return nil
	})

	sc.Step(`^the work is stopped, and the reason is "([^"]*)"$`, func(ctx context.Context, reason string) error {
		one, err := firstWork(ctx)
		if err != nil {
			return err
		}
		if one.GetPhase() != work.PhaseStopped {
			return fmt.Errorf("the work is %q, want stopped", one.GetPhase())
		}
		if one.GetReason() != reason {
			return fmt.Errorf("the reason is %q, want %q", one.GetReason(), reason)
		}
		return nil
	})

	sc.Step(`^the reason on the work is still "([^"]*)"$`, func(ctx context.Context, reason string) error {
		scenario := workFrom(ctx)
		found, err := worldFrom(ctx).client.GetWork(ctx, &quaycrewv1.GetWorkRequest{
			Id: scenario.declared[0].GetId(),
		})
		if err != nil {
			return err
		}
		if found.GetWork().GetReason() != reason {
			return fmt.Errorf("the reason is %q, want %q", found.GetWork().GetReason(), reason)
		}
		return nil
	})

	sc.Step(`^the work carries the moment it finished$`, func(ctx context.Context) error {
		one, err := firstWork(ctx)
		if err != nil {
			return err
		}
		if one.GetFinishedAt() == nil || one.GetFinishedAt().AsTime().IsZero() {
			return fmt.Errorf("the work does not say when it finished")
		}
		return nil
	})

	// The record of what happened is a row beside the row it describes. Nothing is published to the
	// log in this slice, so the store is where it is read from.
	sc.Step(`^the crew holds a "([^"]*)" record for it, naming the (title|reason)$`,
		func(ctx context.Context, kind, naming string) error {
			one, err := firstWork(ctx)
			if err != nil {
				return err
			}
			events, err := worldFrom(ctx).store.ListWorkEvents(ctx, one.GetId())
			if err != nil {
				return err
			}
			want := one.GetTitle()
			if naming == "reason" {
				want = one.GetReason()
			}
			for _, event := range events {
				if event.Kind == kind && event.Detail == want {
					return nil
				}
			}
			return fmt.Errorf("the records are %v, want a %s saying %q", eventKinds(events), kind, want)
		})
}

// declareWork sends one declaration into the project the scenario is standing in and keeps whatever
// came back, answer or refusal.
func declareWork(ctx context.Context, request *quaycrewv1.CreateWorkRequest) error {
	w, scenario := worldFrom(ctx), workFrom(ctx)
	request.Project = w.projectID
	created, err := w.client.CreateWork(ctx, request)
	w.lastErr = err
	if err != nil {
		return nil
	}
	scenario.declared = append(scenario.declared, created.GetWork())
	return nil
}

func listWork(ctx context.Context, request *quaycrewv1.ListWorkRequest) error {
	w := worldFrom(ctx)
	listed, err := w.client.ListWork(ctx, request)
	w.lastErr = err
	if err != nil {
		return err
	}
	workFrom(ctx).listed = listed.GetWork()
	return nil
}

// firstWork is the piece of work the scenario made first, read again so an assertion is about what
// the crew holds rather than about what a call answered.
func firstWork(ctx context.Context) (*quaycrewv1.Work, error) {
	scenario := workFrom(ctx)
	if len(scenario.declared) == 0 {
		return nil, fmt.Errorf("no work has been declared in this scenario")
	}
	return scenario.declared[0], nil
}

func lastWork(ctx context.Context) (*quaycrewv1.Work, error) {
	scenario := workFrom(ctx)
	if len(scenario.declared) == 0 {
		return nil, fmt.Errorf("no work has been declared in this scenario")
	}
	return scenario.declared[len(scenario.declared)-1], nil
}

// refusalSays holds the last refusal to a phrase, so every rule reads the same way in the feature.
func theRefusalSays(want string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the declaration was accepted, and it should have been refused")
		}
		if !strings.Contains(w.lastErr.Error(), want) {
			return fmt.Errorf("the refusal says %q, want it to say %q", w.lastErr, want)
		}
		return nil
	}
}

func eventKinds(events []*work.Event) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

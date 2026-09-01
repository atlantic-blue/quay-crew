package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/cucumber/godog"
)

// A job carries the one sentence it serves, and every job under it carries the same one.
//
// The steps declare through the control plane's real interface, and the two that specify what a
// person sees run the tool in its own process.

type productKey struct{}

// productWorld is the tree this scenario built: the job at the top, the credential a session running
// it holds, and what that session declared.
type productWorld struct {
	tokens   []string
	declared []*quaycrewv1.Job
}

func productFrom(ctx context.Context) *productWorld {
	p, _ := ctx.Value(productKey{}).(*productWorld)
	return p
}

func initializeProductSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, productKey{}, &productWorld{}), nil
	})

	sc.Step(`^a job titled "([^"]*)" saying a person "([^"]*)"$`,
		func(ctx context.Context, title, sentence string) error {
			return aJobAtTheTop(ctx, title, sentence)
		})

	sc.Step(`^a job titled "([^"]*)" saying nothing about what a person gets$`,
		func(ctx context.Context, title string) error {
			return aJobAtTheTop(ctx, title, "")
		})

	sc.Step(`^the session running it declares a job$`, func(ctx context.Context) error {
		return declareUnderTheSession(ctx, 0, "")
	})

	sc.Step(`^the session running it declares a job saying a person "([^"]*)"$`,
		func(ctx context.Context, sentence string) error {
			return declareUnderTheSession(ctx, 0, sentence)
		})

	// The job the session declared is a job like any other, so the system mints a credential for it
	// the same way, and that credential declares one level deeper.
	sc.Step(`^the session running that job declares another$`, func(ctx context.Context) error {
		scenario := productFrom(ctx)
		if len(scenario.declared) == 0 {
			return fmt.Errorf("this scenario declared nothing to go under")
		}
		deeper := scenario.declared[len(scenario.declared)-1]
		token, minted := worldFrom(ctx).server.JobCredentialForTest(ctx, deeper.GetId())
		if !minted {
			return fmt.Errorf("the system minted no credential for the job at depth %d", deeper.GetDepth())
		}
		scenario.tokens = append(scenario.tokens, token)
		return declareUnderTheSession(ctx, len(scenario.tokens)-1, "")
	})

	sc.Step(`^the new job says a person "([^"]*)"$`, func(ctx context.Context, sentence string) error {
		w, scenario := worldFrom(ctx), productFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the declaration was refused: %w", w.lastErr)
		}
		if len(scenario.declared) == 0 {
			return fmt.Errorf("nothing was declared")
		}
		newest := scenario.declared[len(scenario.declared)-1]
		// Read back off the system rather than off the call that answered, because what a session is
		// handed comes off the row.
		found, err := w.client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: newest.GetId()})
		if err != nil {
			return err
		}
		if got := found.GetJob().GetProduct(); got != sentence {
			return fmt.Errorf("the job says a person %q, want %q", got, sentence)
		}
		return nil
	})

	sc.Step(`^the system refuses it, naming the sentence the job above it serves$`,
		theRefusalSays("pastes a link and gets the text back"))

	sc.Step(`^the caller declares a job saying a sentence of (\d+) bytes$`,
		func(ctx context.Context, length int) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: "build the transcript page", Brief: "build what the design describes",
				Product: strings.Repeat("a", length),
			})
		})

	// The whole reason the field exists: the session is given the sentence, and told it wins over
	// whatever design the brief names.
	sc.Step(`^the session was told a person "([^"]*)", and that the sentence wins$`,
		func(ctx context.Context, sentence string) error {
			asked, err := taskAsking(ctx, 0)
			if err != nil {
				return err
			}
			for _, phrase := range []string{sentence, "the sentence wins"} {
				if !strings.Contains(asked, phrase) {
					return fmt.Errorf("the session was asked %q, want it to say %q", asked, phrase)
				}
			}
			return nil
		})

	sc.Step(`^the caller declares a job through the tool saying a person "([^"]*)"$`,
		func(ctx context.Context, sentence string) error {
			return runTool(ctx, "job", "create", whereTheProjectIs(ctx),
				"--title", "build the transcript page", "--brief", "build what the design describes",
				"--product", sentence)
		})

	sc.Step(`^the caller declares a job through the tool saying nothing about what a person gets$`,
		func(ctx context.Context) error {
			return runTool(ctx, "job", "create", whereTheProjectIs(ctx),
				"--title", "build the transcript page", "--brief", "build what the design describes")
		})

	// Carried through to what the person sees next: the tool is run again to read the job back, the
	// way somebody reads it, rather than the declaration being trusted because it exited zero.
	sc.Step(`^reading that job back through the tool says a person "([^"]*)"$`,
		func(ctx context.Context, sentence string) error {
			w := worldFrom(ctx)
			listed, err := w.client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: w.projectID})
			if err != nil {
				return err
			}
			if len(listed.GetJobs()) != 1 {
				return fmt.Errorf("the system holds %d jobs, want 1", len(listed.GetJobs()))
			}
			if err := runTool(ctx, "job", "show", listed.GetJobs()[0].GetId()); err != nil {
				return err
			}
			return says("standard output", toolFrom(ctx).stdout, "for a person: "+sentence)
		})

	// The second half: a run stops once at the first thing a person can open and asks whether it is
	// the product.

	sc.Step(`^the refusal says how to write what a person gets$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("nothing was refused")
		}
		if !strings.Contains(w.lastErr.Error(), "product:") {
			return fmt.Errorf("the refusal says %q, want it to name the line the author has to add", w.lastErr)
		}
		return nil
	})

	sc.Step(`^the flow run asks about the product, naming "([^"]*)" and "([^"]*)"$`,
		func(ctx context.Context, address, sentence string) error {
			w := worldFrom(ctx)
			kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
			if err != nil {
				return err
			}
			if kept.Status != flow.StatusAsking {
				return fmt.Errorf("the run reads back as %q on node %q saying %q, want it asking",
					kept.Status, kept.Node, kept.Reason)
			}
			for _, named := range []string{address, sentence} {
				if !strings.Contains(kept.Question, named) {
					return fmt.Errorf("the question is %q, want it to name %q", kept.Question, named)
				}
			}
			return nil
		})

	sc.Step(`^the job carrying the run says a person "([^"]*)"$`, func(ctx context.Context, sentence string) error {
		w := worldFrom(ctx)
		carrier, err := runCarrier(ctx, w)
		if err != nil {
			return err
		}
		if carrier.Product != sentence {
			return fmt.Errorf("the job carrying the run says a person %q, want %q", carrier.Product, sentence)
		}
		return nil
	})

	// Carried through to what the session doing the next step is handed, rather than stopping at the
	// column. The column is where the sentence is kept; the task is where it does any work.
	sc.Step(`^the step after the question was told a person "([^"]*)", and that the sentence wins$`,
		func(ctx context.Context, sentence string) error {
			asked := worldFrom(ctx).runner.lastRequest().Text
			for _, phrase := range []string{sentence, "the sentence wins"} {
				if !strings.Contains(asked, phrase) {
					return fmt.Errorf("the step after the question was asked %q, want it to say %q", asked, phrase)
				}
			}
			return nil
		})

	sc.Step(`^the flow run stopped because the step named no address$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if kept.Status != flow.StatusStopped {
			return fmt.Errorf("the run reads back as %q on node %q, want it stopped", kept.Status, kept.Node)
		}
		if !strings.Contains(kept.Reason, "address") {
			return fmt.Errorf("the run stopped saying %q, want it to name what was missing", kept.Reason)
		}
		return nil
	})

	sc.Step(`^standard output says the sentence is missing and how to say it$`, func(ctx context.Context) error {
		out := toolFrom(ctx).stdout
		for _, phrase := range []string{"what a person does with what it builds", "--product"} {
			if err := says("standard output", out, phrase); err != nil {
				return err
			}
		}
		return nil
	})
}

// aJobAtTheTop declares the job a tree hangs under, running as a role that may declare jobs of its
// own, and mints the credential a session running it would hold.
//
// The role is what grants: a credential minted for a job that runs as nobody may call nothing, so a
// tree with no role at the top has no second level to specify.
func aJobAtTheTop(ctx context.Context, title, sentence string) error {
	w, scenario := worldFrom(ctx), productFrom(ctx)
	name := "backlog-clearer"
	if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: roleFilesThatMay(name, []string{role.VerbJobCreate, role.VerbJobRead}),
	}); err != nil {
		return err
	}
	if _, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: w.workspaceID, Name: name,
	}); err != nil {
		return err
	}
	declared, err := w.client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: w.projectID, Title: title, Brief: "build what the design describes",
		Role: name, Product: sentence,
	})
	if err != nil {
		return err
	}
	token, minted := w.server.JobCredentialForTest(ctx, declared.GetJob().GetId())
	if !minted {
		return fmt.Errorf("the system minted no credential for the job the session runs")
	}
	scenario.tokens = append(scenario.tokens, token)
	return nil
}

// declareUnderTheSession makes the call a session makes, carrying the credential minted for the job
// it is running.
func declareUnderTheSession(ctx context.Context, which int, sentence string) error {
	w, scenario := worldFrom(ctx), productFrom(ctx)
	if which >= len(scenario.tokens) {
		return fmt.Errorf("this scenario has no session at %d", which)
	}
	declared, err := w.dialAs(scenario.tokens[which]).CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: w.projectID, Title: "decide what the address carries",
		Brief: "decide what the address carries", Role: "backlog-clearer", Product: sentence,
	})
	w.lastErr = err
	if err != nil {
		return nil
	}
	scenario.declared = append(scenario.declared, declared.GetJob())
	return nil
}

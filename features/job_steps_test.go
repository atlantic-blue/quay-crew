package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/role"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/cucumber/godog"
)

// Job is declared intent the system keeps. These steps drive the control plane over its real
// interface, the way the command line does, and read the record back the same way.
//
// Nothing runs the job. What is specified here is the record, the refusals, and that the intent
// outlives the caller that declared it.

type jobKey struct{}

// jobWorld is what one scenario declared.
type jobWorld struct {
	// declared holds each job the scenario made, oldest first.
	declared []*quaycrewv1.Job
	listed   []*quaycrewv1.Job
	// leftOut is what the last declaration answered: the skills the session running that job will
	// start without, because the workspace has not set the secrets they need.
	leftOut []*quaycrewv1.Skill
}

func jobFrom(ctx context.Context) *jobWorld {
	w, _ := ctx.Value(jobKey{}).(*jobWorld)
	return w
}

func initializeJobSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, jobKey{}, &jobWorld{}), nil
	})

	sc.Step(`^a job titled "([^"]*)"$`, func(ctx context.Context, title string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: title, Brief: "open the bill and say when it is due",
		})
	})

	// An answer is written by a controller, and no controller exists yet, so the record is put
	// there the way the controller will write it: on the row.
	sc.Step(`^a job titled "([^"]*)" that answered "([^"]*)"$`,
		func(ctx context.Context, title, answer string) error {
			w, scenario := worldFrom(ctx), jobFrom(ctx)
			declared := &job.Job{
				ID: store.NewID(), Workspace: w.workspaceID, Project: w.projectID,
				Title: title, Brief: "open the bill and say when it is due",
				Version: 1, Phase: job.PhaseDone, Answer: answer,
			}
			if err := w.store.CreateJob(ctx, declared, &job.Event{
				ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID,
				Workspace: w.workspaceID, Project: w.projectID, Detail: title,
				OccurredAt: time.Now().UTC(),
			}); err != nil {
				return err
			}
			found, err := w.client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.ID})
			if err != nil {
				return err
			}
			scenario.declared = append(scenario.declared, found.GetJob())
			return nil
		})

	sc.Step(`^the caller declares a job carrying an identifier of its own$`, func(ctx context.Context) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: "open it", Id: "0123456789abcdef01234567",
		})
	})

	sc.Step(`^the caller declares a job carrying a parent$`, func(ctx context.Context) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: "open it", Parent: "0123456789abcdef01234567",
		})
	})

	sc.Step(`^the caller declares a job with no title$`, func(ctx context.Context) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{Brief: "open it"})
	})

	sc.Step(`^the caller declares a job with a title of (\d+) bytes$`, func(ctx context.Context, bytes int) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: strings.Repeat("t", bytes), Brief: "open it",
		})
	})

	sc.Step(`^the caller declares a job with a brief of (\d+) bytes$`, func(ctx context.Context, bytes int) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: strings.Repeat("b", bytes),
		})
	})

	// A role has to be imported and attached before job can name it, which is the same two steps an
	// operator takes.
	sc.Step(`^the workspace holds the role "([^"]*)" at version (\d+)$`,
		func(ctx context.Context, name string, version int) error {
			w := worldFrom(ctx)
			if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
				Files: roleFiles(name, version, roleManifest{model: "opus", receives: []string{"job"}}),
			}); err != nil {
				return err
			}
			_, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Workspace: w.workspaceID, Name: name,
			})
			return err
		})

	sc.Step(`^the caller declares a job in the role "([^"]*)"$`, func(ctx context.Context, role string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "clear the backlog", Brief: "read the open pull requests", Role: role,
		})
	})

	sc.Step(`^the caller declares a job in the mode "([^"]*)"$`, func(ctx context.Context, mode string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: "open it", Mode: mode,
		})
	})

	sc.Step(`^the caller declares a job expecting the file "([^"]*)"$`, func(ctx context.Context, path string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: "open it", ExpectFile: path,
		})
	})

	sc.Step(`^the caller declares a job after "([^"]*)"$`, func(ctx context.Context, id string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "pay the electricity bill", Brief: "pay it", After: []string{id},
		})
	})

	sc.Step(`^the caller declares a job after the first job$`, func(ctx context.Context) error {
		first, err := firstJob(ctx)
		if err != nil {
			return err
		}
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "pay the electricity bill", Brief: "pay it", After: []string{first.GetId()},
		})
	})

	sc.Step(`^the caller declares a job with a budget of (-?\d+) tokens$`, func(ctx context.Context, tokens int) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: "open it", BudgetTokens: int64(tokens),
		})
	})

	sc.Step(`^the caller declares a job carrying (\d+) labels$`, func(ctx context.Context, count int) error {
		labels := map[string]string{}
		for i := 0; i < count; i++ {
			labels[fmt.Sprintf("key-%d", i)] = "value"
		}
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: "open it", Labels: labels,
		})
	})

	sc.Step(`^the caller declares a job carrying a label value of (\d+) characters$`,
		func(ctx context.Context, size int) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: "read the electricity bill", Brief: "open it",
				Labels: map[string]string{"owner": strings.Repeat("v", size)},
			})
		})

	sc.Step(`^the caller declares a job in a project that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
			Project: "nowhere", Title: "read the electricity bill", Brief: "open it",
		})
		return nil
	})

	sc.Step(`^the caller asks for a job that does not exist$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: "0123456789abcdef01234567"})
		return nil
	})

	// The caller's own context is cancelled, which is what a closed terminal does to the call it
	// was holding. Whatever is read afterwards is read by somebody else entirely.
	sc.Step(`^the caller goes away and the system is asked again$`, func(ctx context.Context) error {
		scenario := jobFrom(ctx)
		if len(scenario.declared) == 0 {
			return fmt.Errorf("no job was declared, so there is nothing to come back to")
		}
		calling, hangUp := context.WithCancel(ctx)
		hangUp()
		w := worldFrom(ctx)
		if _, err := w.client.GetJob(calling, &quaycrewv1.GetJobRequest{Id: scenario.declared[0].GetId()}); err == nil {
			return fmt.Errorf("the call the caller hung up on answered anyway, so the caller never went away")
		}
		found, err := w.client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: scenario.declared[0].GetId()})
		if err != nil {
			return err
		}
		scenario.declared[0] = found.GetJob()
		return nil
	})

	sc.Step(`^the job is still there, pending, with its brief whole$`, func(ctx context.Context) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhasePending {
			return fmt.Errorf("the job is %q, want pending", one.GetPhase())
		}
		if one.GetBrief() != "open the bill and say when it is due" {
			return fmt.Errorf("the brief reads back as %q", one.GetBrief())
		}
		return nil
	})

	// Read off the system rather than off what the declaration answered, so a step that runs after
	// something else has moved the job says what the system holds now.
	sc.Step(`^the job is pending$`, func(ctx context.Context) error {
		return jobIs(ctx, 0, job.PhasePending)
	})

	sc.Step(`^the job is at depth (\d+) with no parent$`, func(ctx context.Context, depth int) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		if one.GetDepth() != int32(depth) || one.GetParent() != "" {
			return fmt.Errorf("the job is at depth %d under %q", one.GetDepth(), one.GetParent())
		}
		return nil
	})

	sc.Step(`^the job carries the moment it was declared$`, func(ctx context.Context) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		if one.GetCreatedAt() == nil || one.GetCreatedAt().AsTime().IsZero() {
			return fmt.Errorf("the job does not say when it was declared")
		}
		return nil
	})

	sc.Step(`^the job carries the role at version (\d+)$`, func(ctx context.Context, version int) error {
		one, err := lastJob(ctx)
		if err != nil {
			return err
		}
		if one.GetRoleVersion() != int32(version) {
			return fmt.Errorf("the job runs as %s at version %d, want version %d",
				one.GetRole(), one.GetRoleVersion(), version)
		}
		return nil
	})

	sc.Step(`^the job waits for the first job$`, func(ctx context.Context) error {
		scenario := jobFrom(ctx)
		if len(scenario.declared) < 2 {
			return fmt.Errorf("%d jobs were declared, want two", len(scenario.declared))
		}
		waits := scenario.declared[1].GetAfter()
		if len(waits) != 1 || waits[0] != scenario.declared[0].GetId() {
			return fmt.Errorf("the job waits for %v, want the first job", waits)
		}
		return nil
	})

	sc.Step(`^the system refuses it and says it assigns the identifier$`, theRefusalSays("assigns the identifier"))
	sc.Step(`^the system refuses it and says the parent comes from the credential$`, theRefusalSays("credential"))
	sc.Step(`^the system refuses it and says a title is needed$`, theRefusalSays("title"))
	sc.Step(`^the system refuses it and says the ceiling is (\d+)$`, func(ctx context.Context, ceiling int) error {
		return theRefusalSays(fmt.Sprintf("%d", ceiling))(ctx)
	})
	sc.Step(`^the system refuses it and names the role$`, theRefusalSays("backlog-clearer"))
	sc.Step(`^the system refuses it and lists the modes$`, func(ctx context.Context) error {
		for _, mode := range []string{"plan", "edits", "dangerous"} {
			if err := theRefusalSays(mode)(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	sc.Step(`^the system refuses it and says the path is read inside the working directory$`,
		theRefusalSays("working directory"))
	sc.Step(`^the system refuses it and says the path climbs out$`, theRefusalSays("climbs out"))
	sc.Step(`^the system refuses it and names the identifier it cannot find$`,
		theRefusalSays("0123456789abcdef01234567"))
	sc.Step(`^the system refuses it and says a budget cannot be below zero$`, theRefusalSays("below zero"))
	sc.Step(`^the system refuses it and says the job already ended$`, theRefusalSays("already"))

	sc.Step(`^the caller lists the job in the project$`, func(ctx context.Context) error {
		return listJob(ctx, &quaycrewv1.ListJobsRequest{Project: worldFrom(ctx).projectID})
	})

	sc.Step(`^the caller lists the job that is pending$`, func(ctx context.Context) error {
		return listJob(ctx, &quaycrewv1.ListJobsRequest{
			Project: worldFrom(ctx).projectID, Phase: job.PhasePending,
		})
	})

	sc.Step(`^the listing holds both jobs, newest first$`, func(ctx context.Context) error {
		scenario := jobFrom(ctx)
		if len(scenario.listed) != 2 {
			return fmt.Errorf("the listing holds %d jobs, want 2", len(scenario.listed))
		}
		if scenario.listed[0].GetId() != scenario.declared[1].GetId() {
			return fmt.Errorf("the listing opens with %q, want the newest first", scenario.listed[0].GetTitle())
		}
		return nil
	})

	sc.Step(`^the listing holds only "([^"]*)"$`, func(ctx context.Context, title string) error {
		scenario := jobFrom(ctx)
		if len(scenario.listed) != 1 {
			return fmt.Errorf("the listing holds %d jobs, want 1", len(scenario.listed))
		}
		if scenario.listed[0].GetTitle() != title {
			return fmt.Errorf("the listing holds %q, want %q", scenario.listed[0].GetTitle(), title)
		}
		return nil
	})

	sc.Step(`^the listing carries the title and not the answer$`, func(ctx context.Context) error {
		scenario := jobFrom(ctx)
		if len(scenario.listed) != 1 {
			return fmt.Errorf("the listing holds %d jobs, want 1", len(scenario.listed))
		}
		if scenario.listed[0].GetAnswer() != "" {
			return fmt.Errorf("the listing carries an answer: %q", scenario.listed[0].GetAnswer())
		}
		if scenario.listed[0].GetTitle() == "" {
			return fmt.Errorf("the listing carries no title, and a title is what a listing is for")
		}
		return nil
	})

	sc.Step(`^reading that one job carries the answer whole$`, func(ctx context.Context) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		found, err := worldFrom(ctx).client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: one.GetId()})
		if err != nil {
			return err
		}
		if found.GetJob().GetAnswer() != "the bill is due on the 14th" {
			return fmt.Errorf("the answer reads back as %q", found.GetJob().GetAnswer())
		}
		return nil
	})

	sc.Step(`^the caller stops the first job saying "([^"]*)"$`, func(ctx context.Context, reason string) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		w := worldFrom(ctx)
		stopped, err := w.client.StopJob(ctx, &quaycrewv1.StopJobRequest{Id: one.GetId(), Reason: reason})
		w.lastErr = err
		if err == nil {
			jobFrom(ctx).declared[0] = stopped.GetJob()
		}
		return nil
	})

	sc.Step(`^the job is stopped, and the reason is "([^"]*)"$`, func(ctx context.Context, reason string) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseStopped {
			return fmt.Errorf("the job is %q, want stopped", one.GetPhase())
		}
		if one.GetReason() != reason {
			return fmt.Errorf("the reason is %q, want %q", one.GetReason(), reason)
		}
		return nil
	})

	sc.Step(`^the reason on the job is still "([^"]*)"$`, func(ctx context.Context, reason string) error {
		scenario := jobFrom(ctx)
		found, err := worldFrom(ctx).client.GetJob(ctx, &quaycrewv1.GetJobRequest{
			Id: scenario.declared[0].GetId(),
		})
		if err != nil {
			return err
		}
		if found.GetJob().GetReason() != reason {
			return fmt.Errorf("the reason is %q, want %q", found.GetJob().GetReason(), reason)
		}
		return nil
	})

	sc.Step(`^the job carries the moment it finished$`, func(ctx context.Context) error {
		one, err := firstJob(ctx)
		if err != nil {
			return err
		}
		if one.GetFinishedAt() == nil || one.GetFinishedAt().AsTime().IsZero() {
			return fmt.Errorf("the job does not say when it finished")
		}
		return nil
	})

	// The record of what happened is a row beside the row it describes. Nothing is published to the
	// log in this slice, so the store is where it is read from.
	sc.Step(`^the system holds a "([^"]*)" record for it, naming the (title|reason)$`,
		func(ctx context.Context, kind, naming string) error {
			one, err := firstJob(ctx)
			if err != nil {
				return err
			}
			events, err := worldFrom(ctx).store.ListJobEvents(ctx, one.GetId())
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

	sc.Step(`^the caller declares a job titled "([^"]*)"$`, func(ctx context.Context, title string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: title, Brief: "clone the repository and push a branch",
		})
	})

	// The sentence is the skill listing's own, so this holds it to naming the skill, the secret and
	// the command that sets it. A note that says a capability is missing without saying how to supply
	// it sends the reader looking, which is the failure this whole scenario is about.
	sc.Step(`^the declaration says the session starts without the "([^"]*)" skill, needing "([^"]*)"$`,
		func(ctx context.Context, name, secret string) error {
			for _, one := range jobFrom(ctx).leftOut {
				if one.GetName() != name {
					continue
				}
				if !strings.Contains(one.GetLeftOut(), secret) {
					return fmt.Errorf("the declaration says the %s skill is left out saying %q, want it to name %q",
						name, one.GetLeftOut(), secret)
				}
				if !strings.Contains(one.GetLeftOut(), "krewe secret set") {
					return fmt.Errorf("the declaration says %q, want it to say how to set the secret",
						one.GetLeftOut())
				}
				return nil
			}
			return fmt.Errorf("the declaration names %v, want it to name the %s skill", leftOutNames(ctx), name)
		})

	sc.Step(`^the declaration names no skill the session starts without$`, func(ctx context.Context) error {
		if named := leftOutNames(ctx); len(named) > 0 {
			return fmt.Errorf("the declaration names %v, want it to name nothing", named)
		}
		return nil
	})
}

// leftOutNames is what the last declaration said the session starts without, for a failure to print.
func leftOutNames(ctx context.Context) []string {
	var names []string
	for _, one := range jobFrom(ctx).leftOut {
		names = append(names, one.GetName())
	}
	return names
}

// declareJob sends one declaration into the project the scenario is standing in and keeps whatever
// came back, answer or refusal.
func declareJob(ctx context.Context, request *quaycrewv1.CreateJobRequest) error {
	w, scenario := worldFrom(ctx), jobFrom(ctx)
	request.Project = w.projectID
	created, err := w.client.CreateJob(ctx, request)
	w.lastErr = err
	if err != nil {
		return nil
	}
	scenario.declared = append(scenario.declared, created.GetJob())
	scenario.leftOut = created.GetLeftOut()
	return nil
}

func listJob(ctx context.Context, request *quaycrewv1.ListJobsRequest) error {
	w := worldFrom(ctx)
	listed, err := w.client.ListJobs(ctx, request)
	w.lastErr = err
	if err != nil {
		return err
	}
	jobFrom(ctx).listed = listed.GetJobs()
	return nil
}

// firstJob is the job the scenario made first, read again so an assertion is about what
// the system holds rather than about what a call answered.
func firstJob(ctx context.Context) (*quaycrewv1.Job, error) {
	scenario := jobFrom(ctx)
	if len(scenario.declared) == 0 {
		return nil, fmt.Errorf("no job has been declared in this scenario")
	}
	return scenario.declared[0], nil
}

func lastJob(ctx context.Context) (*quaycrewv1.Job, error) {
	scenario := jobFrom(ctx)
	if len(scenario.declared) == 0 {
		return nil, fmt.Errorf("no job has been declared in this scenario")
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

func eventKinds(events []*job.Event) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

// The steps for what a job requires of the session that runs it, and the boundary that
// holds it against what its role receives.
func initializeJobMaterialSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the workspace holds the role "([^"]*)" at version (\d+) receiving "([^"]*)"$`,
		func(ctx context.Context, name string, version int, material string) error {
			w := worldFrom(ctx)
			receives := []string{}
			for _, one := range strings.Split(material, ",") {
				receives = append(receives, strings.TrimSpace(one))
			}
			if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
				Files: roleFiles(name, version, roleManifest{model: "opus", receives: receives}),
			}); err != nil {
				return err
			}
			_, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Workspace: w.workspaceID, Name: name,
			})
			return err
		})

	sc.Step(`^the caller declares a job in the role "([^"]*)" requiring "([^"]*)"$`,
		func(ctx context.Context, named, material string) error {
			return declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: "write the tests", Brief: "from the job alone",
				Role: named, Requires: []string{material},
			})
		})

	sc.Step(`^the caller declares a job requiring "([^"]*)"$`, func(ctx context.Context, material string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "read the electricity bill", Brief: "open it", Requires: []string{material},
		})
	})

	// The three parts of a refusal a caller can act on: whose boundary it is, what it does not
	// receive, and the two ways out.
	sc.Step(`^the system refuses it, naming the role, the material and what to change$`,
		func(ctx context.Context) error {
			for _, want := range []string{"test-writer", "context", "import it again", "declare the job without"} {
				if err := theRefusalSays(want)(ctx); err != nil {
					return err
				}
			}
			return nil
		})

	sc.Step(`^the system refuses it and lists the material it hands out$`, func(ctx context.Context) error {
		for _, want := range role.Material {
			if err := theRefusalSays(want)(ctx); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^no job was written$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		if len(listed.GetJobs()) != 0 {
			return fmt.Errorf("the system holds %d jobs, and a refusal writes no row", len(listed.GetJobs()))
		}
		return nil
	})

	// The tool in its own process, because the two things this proves, the exit status and which
	// stream carried the sentence, do not exist inside the test process. It is also the only place
	// the flag itself is specified: everything above declares over the interface, where a flag that
	// was removed cannot be typed at all.
	sc.Step(`^the caller declares a job with "([^"]*)" through the tool$`,
		func(ctx context.Context, flags string) error {
			args := []string{"job", "create", whereTheProjectIs(ctx),
				"--title", "write the tests", "--brief", "from the job alone"}
			return runTool(ctx, append(args, strings.Fields(flags)...)...)
		})

	// Carried through to what the caller sees next: the tool is run again to read the row back, the
	// way a person reads it, rather than the declaration being trusted because it exited zero.
	sc.Step(`^reading that job back says it requires "([^"]*)"$`,
		func(ctx context.Context, material string) error {
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
			return says("standard output", toolFrom(ctx).stdout, "requires "+material)
		})

	sc.Step(`^the job requires "([^"]*)"$`, func(ctx context.Context, material string) error {
		if w := worldFrom(ctx); w.lastErr != nil {
			return fmt.Errorf("the declaration was refused: %v", w.lastErr)
		}
		one, err := lastJob(ctx)
		if err != nil {
			return err
		}
		for _, required := range one.GetRequires() {
			if required == material {
				return nil
			}
		}
		return fmt.Errorf("the job requires %v, want %q among them", one.GetRequires(), material)
	})
}

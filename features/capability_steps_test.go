package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/cucumber/godog"
)

// What a session may do, driven the way a session does it: through a call carrying the credential
// the system minted for the job that session is running.

type capabilityKey struct{}

// capabilityWorld is the session under test and what it declared.
type capabilityWorld struct {
	// running is the job the session is running, and token is the credential minted for it.
	running string
	token   string
	// declared is what that session declared, oldest first.
	declared []*quaycrewv1.Job
	limits   *quaycrewv1.WorkspaceLimits
}

func capabilityFrom(ctx context.Context) *capabilityWorld {
	c, _ := ctx.Value(capabilityKey{}).(*capabilityWorld)
	return c
}

func initializeCapabilitySteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, capabilityKey{}, &capabilityWorld{}), nil
	})

	sc.Step(`^the workspace allows jobs down to depth (\d+)$`, func(ctx context.Context, depth int) error {
		return setLimits(ctx, int32(depth))
	})

	sc.Step(`^the operator allows jobs down to depth (\d+)$`, func(ctx context.Context, depth int) error {
		return setLimits(ctx, int32(depth))
	})

	sc.Step(`^the operator reads the limits of the workspace$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
			Workspace: w.workspaceID,
		})
		w.lastErr = err
		if err != nil {
			return err
		}
		capabilityFrom(ctx).limits = held.GetLimits()
		return nil
	})

	sc.Step(`^the limits allow no depth at all$`, func(ctx context.Context) error {
		if got := capabilityFrom(ctx).limits.GetMaxDepth(); got != 0 {
			return fmt.Errorf("the workspace allows depth %d, want none until somebody raises it", got)
		}
		return nil
	})

	sc.Step(`^the limits say the rest is unset$`, func(ctx context.Context) error {
		held := capabilityFrom(ctx).limits
		if held.GetMaxRunning() != 0 || held.GetBudgetTokens() != 0 || held.GetLeaseSeconds() != 0 {
			return fmt.Errorf("the workspace carries %+v, want every other limit unset", held)
		}
		return nil
	})

	sc.Step(`^the limits allow jobs down to depth (\d+)$`, func(ctx context.Context, depth int) error {
		w := worldFrom(ctx)
		held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
			Workspace: w.workspaceID,
		})
		if err != nil {
			return err
		}
		if got := held.GetLimits().GetMaxDepth(); got != int32(depth) {
			return fmt.Errorf("the workspace allows depth %d, want %d", got, depth)
		}
		return nil
	})

	sc.Step(`^a job titled "([^"]*)" running as a role that may (only read|create) jobs$`,
		func(ctx context.Context, title, grant string) error {
			verbs := []string{role.VerbJobRead}
			if grant == "create" {
				verbs = []string{role.VerbJobCreate, role.VerbJobRead}
			}
			return aSessionRunning(ctx, title, verbs)
		})

	sc.Step(`^that session declares a job$`, func(ctx context.Context) error {
		return declareAsTheSession(ctx, "pull request 341")
	})

	sc.Step(`^that session declared a job$`, func(ctx context.Context) error {
		if err := declareAsTheSession(ctx, "pull request 341"); err != nil {
			return err
		}
		return worldFrom(ctx).lastErr
	})

	sc.Step(`^that session declares a job naming no project$`, func(ctx context.Context) error {
		w, scenario := worldFrom(ctx), capabilityFrom(ctx)
		declared, err := w.dialAs(scenario.token).CreateJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "pull request 341", Brief: "review it", Role: "backlog-clearer",
		})
		w.lastErr = err
		if err != nil {
			return nil
		}
		scenario.declared = append(scenario.declared, declared.GetJob())
		return nil
	})

	sc.Step(`^the new job is in the same project as the job that declared it$`, func(ctx context.Context) error {
		w, scenario := worldFrom(ctx), capabilityFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the declaration was refused: %w", w.lastErr)
		}
		newest := scenario.declared[len(scenario.declared)-1]
		if newest.GetProject() != w.projectID {
			return fmt.Errorf("the job landed in project %q, want %q, which is the project its credential names",
				newest.GetProject(), w.projectID)
		}
		return nil
	})

	// The job the session declared is itself a job, so the system mints a credential for it
	// the same way, and that credential is what declares one level deeper.
	sc.Step(`^the job at depth (\d+) declares another$`, func(ctx context.Context, depth int) error {
		scenario := capabilityFrom(ctx)
		if len(scenario.declared) == 0 {
			return fmt.Errorf("this scenario declared nothing at depth %d", depth)
		}
		deeper := scenario.declared[len(scenario.declared)-1]
		if deeper.GetDepth() != int32(depth) {
			return fmt.Errorf("the job is at depth %d, want %d", deeper.GetDepth(), depth)
		}
		token, minted := worldFrom(ctx).server.JobCredentialForTest(ctx, deeper.GetId())
		if !minted {
			return fmt.Errorf("the system minted no credential for the job at depth %d", depth)
		}
		return declareCarrying(ctx, token, "write a test")
	})

	sc.Step(`^the new job hangs under the job that declared it, one level deeper$`, func(ctx context.Context) error {
		scenario := capabilityFrom(ctx)
		if worldFrom(ctx).lastErr != nil {
			return fmt.Errorf("the declaration was refused: %w", worldFrom(ctx).lastErr)
		}
		if len(scenario.declared) == 0 {
			return fmt.Errorf("nothing was declared")
		}
		newest := scenario.declared[len(scenario.declared)-1]
		if newest.GetParent() == "" {
			return fmt.Errorf("the job has no parent, so nothing bounds how deep it goes")
		}
		parent, err := worldFrom(ctx).client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: newest.GetParent()})
		if err != nil {
			return err
		}
		if newest.GetDepth() != parent.GetJob().GetDepth()+1 {
			return fmt.Errorf("the job is at depth %d under job at depth %d",
				newest.GetDepth(), parent.GetJob().GetDepth())
		}
		return nil
	})

	sc.Step(`^the system refuses it and names the verb it lacks$`, func(ctx context.Context) error {
		return theRefusalSays(role.VerbJobCreate)(ctx)
	})

	sc.Step(`^the system refuses it and names the limit and the command that raises it$`, func(ctx context.Context) error {
		for _, want := range []string{"no deeper than", "quay limits"} {
			if err := theRefusalSays(want)(ctx); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the project holds only the job the operator declared$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		if len(listed.GetJobs()) != 1 {
			return fmt.Errorf("the project holds %d jobs, want the one the operator declared",
				len(listed.GetJobs()))
		}
		return nil
	})

	sc.Step(`^that session tries to raise the ceiling$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = asTheSession(ctx).SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
			Limits: &quaycrewv1.WorkspaceLimits{Workspace: w.workspaceID, MaxDepth: 9},
		})
		return nil
	})

	sc.Step(`^that session tries to attach a hook$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = asTheSession(ctx).AttachHook(ctx, &quaycrewv1.AttachHookRequest{
			Workspace: w.workspaceID, Name: "prompt-analyser",
		})
		return nil
	})

	sc.Step(`^that session tries to set a secret$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = asTheSession(ctx).SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Workspace: w.workspaceID, Key: "CLAUDE_CODE_OAUTH_TOKEN", Value: "tok-xyz",
		})
		return nil
	})

	// What a task is actually handed, read off the task the system ran rather than off the sandbox: a
	// credential is minted for one job and travels in the environment of one task, because a
	// sandbox keeps what it was born with and would otherwise label every later task with the first
	// task's grant.
	sc.Step(`^the system runs that job$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: w.projectID, Text: "clear it", Job: capabilityFrom(ctx).running,
		})
		w.lastErr = err
		return err
	})

	sc.Step(`^the task carries the address of the system$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if got := w.runner.lastRequest().Env["QC_GRPC_ADDR"]; got != w.reachable {
			return fmt.Errorf("the task was told the system is at %q, want %q", got, w.reachable)
		}
		return nil
	})

	sc.Step(`^the task carries the credential minted for that job, not the operator's token$`,
		func(ctx context.Context) error {
			w, scenario := worldFrom(ctx), capabilityFrom(ctx)
			presented := w.runner.lastRequest().Env["QC_TOKEN"]
			if presented == "" {
				return fmt.Errorf("the task carries no credential, so it can do nothing at the address it was given")
			}
			if presented == w.token || presented == w.driverToken {
				return fmt.Errorf("the task carries a token that is not its own, so it holds what the system holds")
			}
			grant, recognised := w.server.Grants().Grant(presented)
			if !recognised {
				return fmt.Errorf("the system does not recognise the credential it put in the task")
			}
			if grant.Job != scenario.running {
				return fmt.Errorf("the credential is bound to %q, want the job the task runs for", grant.Job)
			}
			return nil
		})

	sc.Step(`^the task carries no address and no token$`, func(ctx context.Context) error {
		env := worldFrom(ctx).runner.lastRequest().Env
		for _, name := range []string{"QC_GRPC_ADDR", "QC_TOKEN"} {
			if got, told := env[name]; told {
				return fmt.Errorf("the task carries %s=%q, and a task running no job is told neither", name, got)
			}
		}
		return nil
	})

	sc.Step(`^that session tries to stop the job it is running$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = asTheSession(ctx).StopJob(ctx, &quaycrewv1.StopJobRequest{
			Id: capabilityFrom(ctx).running, Reason: "I have had enough",
		})
		return nil
	})

	sc.Step(`^the system refuses it and names the verb it lacks and how an operator grants it$`,
		func(ctx context.Context) error {
			for _, want := range []string{role.VerbJobStop, "verbs list", "attaching it"} {
				if err := theRefusalSays(want)(ctx); err != nil {
					return err
				}
			}
			return nil
		})

	sc.Step(`^the job is still running$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		held, err := w.client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: capabilityFrom(ctx).running})
		if err != nil {
			return err
		}
		if held.GetJob().GetPhase() == job.PhaseStopped {
			return fmt.Errorf("the job is stopped, and the session that asked was refused")
		}
		return nil
	})

	sc.Step(`^the system refuses the session that call$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the session was allowed the call")
		}
		if !strings.Contains(w.lastErr.Error(), "may call the job verbs and nothing else") &&
			!strings.Contains(w.lastErr.Error(), "may not") {
			return fmt.Errorf("the refusal says %q, want it to say what a session may do", w.lastErr)
		}
		return nil
	})

	sc.Step(`^the credential names that job, carries only the verbs the role declared, and runs out$`,
		func(ctx context.Context) error {
			scenario := capabilityFrom(ctx)
			grant, recognised := worldFrom(ctx).server.Grants().Grant(scenario.token)
			if !recognised {
				return fmt.Errorf("the system does not recognise the credential it minted")
			}
			if grant.Job != scenario.running {
				return fmt.Errorf("the credential is bound to %q, want the job the session is running", grant.Job)
			}
			for _, verb := range []string{role.VerbJobCreate, role.VerbJobRead} {
				if !grant.May(verb) {
					return fmt.Errorf("the credential may not %s, and the role declared it", verb)
				}
			}
			for _, verb := range []string{role.VerbJobStop, role.VerbJobAnswer} {
				if grant.May(verb) {
					return fmt.Errorf("the credential may %s, and the role never declared it", verb)
				}
			}
			// It ends, and it ends far enough out to cover the job. The two bounds are the whole of the
			// lifetime: an end nobody set is a credential that works forever, and an end set by the
			// system's hold on the job is sixty seconds, which is less than a job takes.
			if grant.ExpiresAt.IsZero() {
				return fmt.Errorf("the credential never runs out, so one read out of a container works forever")
			}
			if lasts := time.Until(grant.ExpiresAt); lasts <= job.DefaultLease {
				return fmt.Errorf("the credential lasts %s, which is the system's hold on the job: a session "+
					"could not declare anything after that, and a job takes minutes", lasts.Round(time.Second))
			}
			if grant.ExpiresAt.After(time.Now().Add(24 * time.Hour)) {
				return fmt.Errorf("the credential runs to %v, and one that leaks out of a container should "+
					"not last into the week", grant.ExpiresAt)
			}
			return nil
		})

	// The clock the system reads a credential's life against, moved rather than waited out. Nothing
	// else in the system reads it, so a scenario half an hour into a job is otherwise the same system.
	sc.Step(`^that job has been running for (\d+) (minutes|days)$`,
		func(ctx context.Context, count int, unit string) error {
			every := time.Minute
			if unit == "days" {
				every = 24 * time.Hour
			}
			worldFrom(ctx).clockAhead.Store(int64(time.Duration(count) * every))
			return nil
		})

	sc.Step(`^the operator stops that job$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, err := w.client.StopJob(ctx, &quaycrewv1.StopJobRequest{
			Id: capabilityFrom(ctx).running, Reason: "I have had enough",
		})
		return err
	})

	sc.Step(`^the system refuses it, says the credential ran out, and says when$`,
		func(ctx context.Context) error {
			grant, recognised := worldFrom(ctx).server.Grants().Grant(capabilityFrom(ctx).token)
			if !recognised {
				return fmt.Errorf("the system has forgotten the credential, so it cannot say what became of it")
			}
			for _, want := range []string{"ran out at", grant.ExpiresAt.UTC().Format(time.RFC3339)} {
				if err := theRefusalSays(want)(ctx); err != nil {
					return err
				}
			}
			if strings.Contains(worldFrom(ctx).lastErr.Error(), "not this system's") {
				return fmt.Errorf("the refusal says %q, and it was this system's: it had run out",
					worldFrom(ctx).lastErr)
			}
			return nil
		})

	sc.Step(`^the system refuses it and names the job that ended and the phase it ended in$`,
		func(ctx context.Context) error {
			for _, want := range []string{capabilityFrom(ctx).running, job.PhaseStopped} {
				if err := theRefusalSays(want)(ctx); err != nil {
					return err
				}
			}
			return nil
		})
	sc.Step(`^the driver is refused importing, listing, attaching and detaching a hook$`,
		func(context.Context) error {
			for _, method := range []string{
				quaycrewv1.ControlPlaneService_ImportHook_FullMethodName,
				quaycrewv1.ControlPlaneService_ListHooks_FullMethodName,
				quaycrewv1.ControlPlaneService_AttachHook_FullMethodName,
				quaycrewv1.ControlPlaneService_DetachHook_FullMethodName,
			} {
				if err := controlplane.DeniedToDriver(method, nil); err == nil {
					return fmt.Errorf("the driver is allowed %s", method)
				}
			}
			return nil
		})
}

// setLimits writes the workspace's ceiling as the operator.
func setLimits(ctx context.Context, depth int32) error {
	w := worldFrom(ctx)
	held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{Workspace: w.workspaceID})
	if err != nil {
		return err
	}
	asked := held.GetLimits()
	asked.MaxDepth = depth
	_, err = w.client.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{Limits: asked})
	w.lastErr = err
	return err
}

// aSessionRunning declares one job as the operator, gives it a role with the verbs named,
// and mints the credential a session running it would hold.
func aSessionRunning(ctx context.Context, title string, verbs []string) error {
	w, scenario := worldFrom(ctx), capabilityFrom(ctx)
	name := "backlog-clearer"
	if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: roleFilesThatMay(name, verbs),
	}); err != nil {
		return err
	}
	if _, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: w.workspaceID, Name: name,
	}); err != nil {
		return err
	}
	declared, err := w.client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: w.projectID, Title: title, Brief: "read the open pull requests", Role: name,
	})
	if err != nil {
		return err
	}
	scenario.running = declared.GetJob().GetId()
	token, minted := w.server.JobCredentialForTest(ctx, scenario.running)
	if !minted {
		return fmt.Errorf("the system minted no credential for the job the session runs")
	}
	scenario.token = token
	return nil
}

// declareAsTheSession makes the call the session makes, carrying its own credential.
func declareAsTheSession(ctx context.Context, title string) error {
	return declareCarrying(ctx, capabilityFrom(ctx).token, title)
}

// declareCarrying declares a job as whoever holds this credential.
func declareCarrying(ctx context.Context, token, title string) error {
	w, scenario := worldFrom(ctx), capabilityFrom(ctx)
	// The child runs as the same role, because a role is what grants: a job declared without one
	// holds a credential that may call nothing, and could declare nothing itself.
	declared, err := w.dialAs(token).CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: w.projectID, Title: title, Brief: "review it", Role: "backlog-clearer",
	})
	w.lastErr = err
	if err != nil {
		return nil
	}
	scenario.declared = append(scenario.declared, declared.GetJob())
	return nil
}

// asTheSession is a client presenting the session's own credential, which is how a session's calls
// go through the same guard a real one's do.
func asTheSession(ctx context.Context) quaycrewv1.ControlPlaneServiceClient {
	return worldFrom(ctx).dialAs(capabilityFrom(ctx).token)
}

// roleFilesThatMay is a role declaring the verbs it may call.
func roleFilesThatMay(name string, verbs []string) []*quaycrewv1.RoleFile {
	manifest := fmt.Sprintf("name: %s\nversion: 1\nsummary: clears the backlog\nmodel: opus\nreceives:\n  - job\n", name)
	if len(verbs) > 0 {
		manifest += "verbs:\n"
		for _, verb := range verbs {
			manifest += "  - " + verb + "\n"
		}
	}
	return []*quaycrewv1.RoleFile{
		{Path: role.ManifestFile, Body: []byte(manifest)},
		{Path: role.BriefFile, Body: []byte("Read the open pull requests.")},
	}
}

var _ = auth.Grant{}

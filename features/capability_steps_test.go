package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/atlantic-blue/quay-crew/internal/work"
	"github.com/cucumber/godog"
)

// What a session may do, driven the way a session does it: through a call carrying the credential
// the crew minted for the piece of work that session is running.

type capabilityKey struct{}

// capabilityWorld is the session under test and what it declared.
type capabilityWorld struct {
	// running is the piece of work the session is running, and token is the credential minted for it.
	running string
	token   string
	// declared is what that session declared, oldest first.
	declared []*quaycrewv1.Work
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

	sc.Step(`^the workspace allows work down to depth (\d+)$`, func(ctx context.Context, depth int) error {
		return setLimits(ctx, int32(depth))
	})

	sc.Step(`^the operator allows work down to depth (\d+)$`, func(ctx context.Context, depth int) error {
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

	sc.Step(`^the limits allow work down to depth (\d+)$`, func(ctx context.Context, depth int) error {
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

	sc.Step(`^a piece of work titled "([^"]*)" running as a role that may (only read|create) work$`,
		func(ctx context.Context, title, grant string) error {
			verbs := []string{role.VerbWorkRead}
			if grant == "create" {
				verbs = []string{role.VerbWorkCreate, role.VerbWorkRead}
			}
			return aSessionRunning(ctx, title, verbs)
		})

	sc.Step(`^that session declares a piece of work$`, func(ctx context.Context) error {
		return declareAsTheSession(ctx, "pull request 341")
	})

	sc.Step(`^that session declared a piece of work$`, func(ctx context.Context) error {
		if err := declareAsTheSession(ctx, "pull request 341"); err != nil {
			return err
		}
		return worldFrom(ctx).lastErr
	})

	sc.Step(`^that session declares a piece of work naming no project$`, func(ctx context.Context) error {
		w, scenario := worldFrom(ctx), capabilityFrom(ctx)
		declared, err := w.dialAs(scenario.token).CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: "pull request 341", Brief: "review it", Role: "backlog-clearer",
		})
		w.lastErr = err
		if err != nil {
			return nil
		}
		scenario.declared = append(scenario.declared, declared.GetWork())
		return nil
	})

	sc.Step(`^the new work is in the same project as the work that declared it$`, func(ctx context.Context) error {
		w, scenario := worldFrom(ctx), capabilityFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the declaration was refused: %w", w.lastErr)
		}
		newest := scenario.declared[len(scenario.declared)-1]
		if newest.GetProject() != w.projectID {
			return fmt.Errorf("the work landed in project %q, want %q, which is the project its credential names",
				newest.GetProject(), w.projectID)
		}
		return nil
	})

	// The work the session declared is itself a piece of work, so the crew mints a credential for it
	// the same way, and that credential is what declares one level deeper.
	sc.Step(`^the work at depth (\d+) declares another$`, func(ctx context.Context, depth int) error {
		scenario := capabilityFrom(ctx)
		if len(scenario.declared) == 0 {
			return fmt.Errorf("this scenario declared nothing at depth %d", depth)
		}
		deeper := scenario.declared[len(scenario.declared)-1]
		if deeper.GetDepth() != int32(depth) {
			return fmt.Errorf("the work is at depth %d, want %d", deeper.GetDepth(), depth)
		}
		token, minted := worldFrom(ctx).server.WorkCredentialForTest(ctx, deeper.GetId())
		if !minted {
			return fmt.Errorf("the crew minted no credential for the work at depth %d", depth)
		}
		return declareCarrying(ctx, token, "write a test")
	})

	sc.Step(`^the new work hangs under the work that declared it, one level deeper$`, func(ctx context.Context) error {
		scenario := capabilityFrom(ctx)
		if worldFrom(ctx).lastErr != nil {
			return fmt.Errorf("the declaration was refused: %w", worldFrom(ctx).lastErr)
		}
		if len(scenario.declared) == 0 {
			return fmt.Errorf("nothing was declared")
		}
		newest := scenario.declared[len(scenario.declared)-1]
		if newest.GetParent() == "" {
			return fmt.Errorf("the work has no parent, so nothing bounds how deep it goes")
		}
		parent, err := worldFrom(ctx).client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: newest.GetParent()})
		if err != nil {
			return err
		}
		if newest.GetDepth() != parent.GetWork().GetDepth()+1 {
			return fmt.Errorf("the work is at depth %d under work at depth %d",
				newest.GetDepth(), parent.GetWork().GetDepth())
		}
		return nil
	})

	sc.Step(`^the crew refuses it and names the verb it lacks$`, func(ctx context.Context) error {
		return theRefusalSays(role.VerbWorkCreate)(ctx)
	})

	sc.Step(`^the crew refuses it and names the limit and the command that raises it$`, func(ctx context.Context) error {
		for _, want := range []string{"no deeper than", "quay limits"} {
			if err := theRefusalSays(want)(ctx); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the project holds only the work the operator declared$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListWork(ctx, &quaycrewv1.ListWorkRequest{Project: w.projectID})
		if err != nil {
			return err
		}
		if len(listed.GetWork()) != 1 {
			return fmt.Errorf("the project holds %d pieces of work, want the one the operator declared",
				len(listed.GetWork()))
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

	// What a task is actually handed, read off the task the crew ran rather than off the sandbox: a
	// credential is minted for one piece of work and travels in the environment of one task, because a
	// sandbox keeps what it was born with and would otherwise label every later task with the first
	// task's grant.
	sc.Step(`^the crew runs that work$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, err := w.client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: w.projectID, Text: "clear it", Work: capabilityFrom(ctx).running,
		})
		w.lastErr = err
		return err
	})

	sc.Step(`^the task carries the address of the crew$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if got := w.runner.lastRequest().Env["QC_GRPC_ADDR"]; got != w.reachable {
			return fmt.Errorf("the task was told the crew is at %q, want %q", got, w.reachable)
		}
		return nil
	})

	sc.Step(`^the task carries the credential minted for that work, not the operator's token$`,
		func(ctx context.Context) error {
			w, scenario := worldFrom(ctx), capabilityFrom(ctx)
			presented := w.runner.lastRequest().Env["QC_TOKEN"]
			if presented == "" {
				return fmt.Errorf("the task carries no credential, so it can do nothing at the address it was given")
			}
			if presented == w.token || presented == w.driverToken {
				return fmt.Errorf("the task carries a token that is not its own, so it holds what the crew holds")
			}
			grant, recognised := w.server.Grants().Grant(presented)
			if !recognised {
				return fmt.Errorf("the crew does not recognise the credential it put in the task")
			}
			if grant.Work != scenario.running {
				return fmt.Errorf("the credential is bound to %q, want the work the task runs for", grant.Work)
			}
			return nil
		})

	sc.Step(`^the task carries no address and no token$`, func(ctx context.Context) error {
		env := worldFrom(ctx).runner.lastRequest().Env
		for _, name := range []string{"QC_GRPC_ADDR", "QC_TOKEN"} {
			if got, told := env[name]; told {
				return fmt.Errorf("the task carries %s=%q, and a task running no work is told neither", name, got)
			}
		}
		return nil
	})

	sc.Step(`^that session tries to stop the work it is running$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = asTheSession(ctx).StopWork(ctx, &quaycrewv1.StopWorkRequest{
			Id: capabilityFrom(ctx).running, Reason: "I have had enough",
		})
		return nil
	})

	sc.Step(`^the crew refuses it and names the verb it lacks and how an operator grants it$`,
		func(ctx context.Context) error {
			for _, want := range []string{role.VerbWorkStop, "may list", "attaching it"} {
				if err := theRefusalSays(want)(ctx); err != nil {
					return err
				}
			}
			return nil
		})

	sc.Step(`^the work is still running$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		held, err := w.client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: capabilityFrom(ctx).running})
		if err != nil {
			return err
		}
		if held.GetWork().GetPhase() == work.PhaseStopped {
			return fmt.Errorf("the work is stopped, and the session that asked was refused")
		}
		return nil
	})

	sc.Step(`^the crew refuses the session that call$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the session was allowed the call")
		}
		if !strings.Contains(w.lastErr.Error(), "may call the work verbs and nothing else") &&
			!strings.Contains(w.lastErr.Error(), "may not") {
			return fmt.Errorf("the refusal says %q, want it to say what a session may do", w.lastErr)
		}
		return nil
	})

	sc.Step(`^the credential names that work, carries only the verbs the role declared, and runs out$`,
		func(ctx context.Context) error {
			scenario := capabilityFrom(ctx)
			grant, recognised := worldFrom(ctx).server.Grants().Grant(scenario.token)
			if !recognised {
				return fmt.Errorf("the crew does not recognise the credential it minted")
			}
			if grant.Work != scenario.running {
				return fmt.Errorf("the credential is bound to %q, want the work the session is running", grant.Work)
			}
			for _, verb := range []string{role.VerbWorkCreate, role.VerbWorkRead} {
				if !grant.May(verb) {
					return fmt.Errorf("the credential may not %s, and the role declared it", verb)
				}
			}
			for _, verb := range []string{role.VerbWorkStop, role.VerbWorkAnswer} {
				if grant.May(verb) {
					return fmt.Errorf("the credential may %s, and the role never declared it", verb)
				}
			}
			if grant.ExpiresAt.IsZero() || grant.ExpiresAt.After(time.Now().Add(2*time.Hour)) {
				return fmt.Errorf("the credential runs to %v, want an end close enough to matter", grant.ExpiresAt)
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

// aSessionRunning declares one piece of work as the operator, gives it a role with the verbs named,
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
	declared, err := w.client.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: w.projectID, Title: title, Brief: "read the open pull requests", Role: name,
	})
	if err != nil {
		return err
	}
	scenario.running = declared.GetWork().GetId()
	token, minted := w.server.WorkCredentialForTest(ctx, scenario.running)
	if !minted {
		return fmt.Errorf("the crew minted no credential for the work the session runs")
	}
	scenario.token = token
	return nil
}

// declareAsTheSession makes the call the session makes, carrying its own credential.
func declareAsTheSession(ctx context.Context, title string) error {
	return declareCarrying(ctx, capabilityFrom(ctx).token, title)
}

// declareCarrying declares a piece of work as whoever holds this credential.
func declareCarrying(ctx context.Context, token, title string) error {
	w, scenario := worldFrom(ctx), capabilityFrom(ctx)
	// The child runs as the same role, because a role is what grants: work declared without one
	// holds a credential that may call nothing, and could declare nothing itself.
	declared, err := w.dialAs(token).CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: w.projectID, Title: title, Brief: "review it", Role: "backlog-clearer",
	})
	w.lastErr = err
	if err != nil {
		return nil
	}
	scenario.declared = append(scenario.declared, declared.GetWork())
	return nil
}

// asTheSession is a client presenting the session's own credential, which is how a session's calls
// go through the same guard a real one's do.
func asTheSession(ctx context.Context) quaycrewv1.ControlPlaneServiceClient {
	return worldFrom(ctx).dialAs(capabilityFrom(ctx).token)
}

// roleFilesThatMay is a role declaring the verbs it may call.
func roleFilesThatMay(name string, verbs []string) []*quaycrewv1.RoleFile {
	manifest := fmt.Sprintf("name: %s\nversion: 1\nsummary: clears the backlog\nmodel: opus\nreceives:\n  - work\n", name)
	if len(verbs) > 0 {
		manifest += "may:\n"
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

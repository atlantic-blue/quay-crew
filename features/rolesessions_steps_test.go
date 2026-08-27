package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/cucumber/godog"
)

// A step of a flow that runs as a role, driven over the same road everything else takes: the graph
// is imported, the run is started, and what the role's session was given is read off the host.
func initializeRoleSessionSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator imported the "([^"]*)" role, which receives only work$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
				Files: roleFiles(name, 1, roleManifest{model: "opus", receives: []string{"work"}}),
			})
			w.lastErr = err
			return err
		})

	sc.Step(`^the "([^"]*)" step ran in a session of its own$`, func(ctx context.Context, node string) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		own, asRole := kept.State[flow.SessionKey], kept.State["session."+node]
		if asRole == "" {
			return fmt.Errorf("the run remembers no session for the %s step: %v", node, kept.State)
		}
		if asRole == own {
			return fmt.Errorf("the %s step ran in the run's own session", node)
		}
		return nil
	})

	sc.Step(`^that session was asked "([^"]*)"$`, func(ctx context.Context, prompt string) error {
		tasks, err := roleSessionTasks(ctx)
		if err != nil {
			return err
		}
		if len(tasks) != 1 {
			return fmt.Errorf("the role's session holds %d tasks, want 1", len(tasks))
		}
		if tasks[0].GetPrompt() != prompt {
			return fmt.Errorf("the role's session was asked %q, want %q", tasks[0].GetPrompt(), prompt)
		}
		return nil
	})

	sc.Step(`^the crew built (\d+) sandboxe?s?$`, func(ctx context.Context, want int) error {
		if got := len(worldFrom(ctx).provider.Created); got != want {
			return fmt.Errorf("the crew built %d sandboxes, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the role's sandbox is not the run's own$`, func(ctx context.Context) error {
		box, err := roleSandbox(ctx)
		if err != nil {
			return err
		}
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if box.ID == kept.State[flow.SessionKey] {
			return fmt.Errorf("the role ran in the container the run's own session is in")
		}
		if box.Role == "" {
			return fmt.Errorf("the role's sandbox does not know which role it is for, so its state would go where every other session's does")
		}
		return nil
	})

	// The shared conversation store holds every transcript in the workspace, so a role kept there
	// could read what the session it must not see said.
	sc.Step(`^the role's session keeps its conversation to itself$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		box, err := roleSandbox(ctx)
		if err != nil {
			return err
		}
		shared := filepath.Join(w.storage.Dir, "workspaces", w.workspaceID, "claude")
		for _, mount := range w.storage.MyDirs(box) {
			if mount == shared {
				return fmt.Errorf("the role's session reads the workspace's own conversation store at %s", shared)
			}
		}
		return nil
	})

	sc.Step(`^the role's memory file carries "([^"]*)"$`, func(ctx context.Context, want string) error {
		body, err := roleMemory(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(body, want) {
			return fmt.Errorf("the role's memory file reads %q, want it to carry %q", body, want)
		}
		return nil
	})

	sc.Step(`^the role's memory file does not carry "([^"]*)"$`, func(ctx context.Context, absent string) error {
		body, err := roleMemory(ctx)
		if err != nil {
			return err
		}
		if strings.Contains(body, absent) {
			return fmt.Errorf("the role's memory file carries %q, which its role does not receive:\n%s",
				absent, body)
		}
		return nil
	})

	sc.Step(`^the role's session holds no skills$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		session, err := roleSession(ctx)
		if err != nil {
			return err
		}
		listed, err := w.client.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Session: session.GetId()})
		if err != nil {
			return err
		}
		if len(listed.GetSkills()) != 0 {
			return fmt.Errorf("the role's session holds %d skills", len(listed.GetSkills()))
		}
		return nil
	})

	sc.Step(`^the role's sandbox does not mount the ([^ ]+) skill$`, func(ctx context.Context, name string) error {
		box, err := roleSandbox(ctx)
		if err != nil {
			return err
		}
		at := skill.DirIn(sandbox.SkillsPath, name)
		for _, mount := range box.Mounts {
			if mount.Target == at {
				return fmt.Errorf("the role's sandbox mounts %s, which its role does not receive", at)
			}
		}
		return nil
	})

	// Appending is what a session writing a note actually does, and the directory is mounted in, so
	// this process and that container are looking at one place.
	sc.Step(`^the role's session writes "([^"]*)" into its memory$`, func(ctx context.Context, note string) error {
		box, err := roleSandbox(ctx)
		if err != nil {
			return err
		}
		dirs := worldFrom(ctx).storage.MyDirs(box)
		if len(dirs) == 0 {
			return fmt.Errorf("the role's session has no memory directory")
		}
		body, _ := sandbox.ReadMemory(dirs[0])
		return os.WriteFile(filepath.Join(dirs[0], sandbox.MemoryFile),
			[]byte(body+"\n\n"+note+"\n"), 0o644)
	})

	sc.Step(`^the workspace context does not carry "([^"]*)"$`, func(ctx context.Context, absent string) error {
		w := worldFrom(ctx)
		for _, held := range []struct {
			scope store.ContextScope
			owner string
		}{{store.ContextCrew, ""}, {store.ContextWorkspace, w.workspaceID}} {
			stored, err := w.store.GetContext(ctx, held.scope, held.owner)
			if err != nil {
				continue
			}
			if strings.Contains(stored, absent) {
				return fmt.Errorf("the %s context carries %q, written by a session that was given none of it",
					held.scope, absent)
			}
		}
		return nil
	})

	sc.Step(`^every session the run started is archived$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		started := flow.SessionsIn(kept.State)
		if len(started) < 2 {
			return fmt.Errorf("the run started %d sessions, so this proves nothing", len(started))
		}
		for _, id := range started {
			session, err := w.store.GetSession(ctx, id)
			if err != nil {
				return err
			}
			if session.GetArchivedAt() == nil {
				return fmt.Errorf("session %s is still live, so a finished run left a container behind", id)
			}
		}
		return nil
	})
}

// roleSession is the session the run's role step ran in.
func roleSession(ctx context.Context) (*quaycrewv1.Session, error) {
	w := worldFrom(ctx)
	kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range flow.SessionsIn(kept.State) {
		session, err := w.store.GetSession(ctx, id)
		if err != nil {
			return nil, err
		}
		if session.GetRole() != "" {
			return session, nil
		}
	}
	return nil, fmt.Errorf("the run started no session running as a role: %v", kept.State)
}

// roleSandbox is the configuration the crew built the role's container from.
func roleSandbox(ctx context.Context) (sandbox.Config, error) {
	session, err := roleSession(ctx)
	if err != nil {
		return sandbox.Config{}, err
	}
	for _, built := range worldFrom(ctx).provider.Created {
		if built.ID == session.GetId() {
			return built, nil
		}
	}
	return sandbox.Config{}, fmt.Errorf("no sandbox was built for the role's session")
}

// roleMemory is the memory file the role's session reads, which is its own rather than the
// workspace's.
func roleMemory(ctx context.Context) (string, error) {
	box, err := roleSandbox(ctx)
	if err != nil {
		return "", err
	}
	dirs := worldFrom(ctx).storage.MyDirs(box)
	if len(dirs) == 0 {
		return "", fmt.Errorf("the role's session has no memory directory")
	}
	body, found := sandbox.ReadMemory(dirs[0])
	if !found {
		return "", fmt.Errorf("nothing was written to the role's memory file at all")
	}
	return body, nil
}

func roleSessionTasks(ctx context.Context) ([]*quaycrewv1.Task, error) {
	w := worldFrom(ctx)
	session, err := roleSession(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session.GetId()})
	if err != nil {
		return nil, err
	}
	return resp.GetTasks(), nil
}

// A refusal that only says no leaves the operator guessing which of a graph's steps to fix, so the
// reason on the run has to name the role nobody attached.
func initializeStoppedReasonSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the run stopped saying "([^"]*)"$`, func(ctx context.Context, named string) error {
		w := worldFrom(ctx)
		kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
		if err != nil {
			return err
		}
		if !strings.Contains(kept.Reason, named) {
			return fmt.Errorf("the run stopped saying %q, want it to name %q", kept.Reason, named)
		}
		return nil
	})
}

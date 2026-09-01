package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/manual"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/cucumber/godog"
)

type contextWorld struct {
	dirs []*quaycrewv1.ContextDir
}

type contextKey struct{}

func contextFrom(ctx context.Context) *contextWorld {
	c, _ := ctx.Value(contextKey{}).(*contextWorld)
	return c
}

// scoped returns the listed directories of one scope.
func (c *contextWorld) scoped(scope string) []*quaycrewv1.ContextDir {
	out := make([]*quaycrewv1.ContextDir, 0, len(c.dirs))
	for _, dir := range c.dirs {
		if dir.GetScope() == scope {
			out = append(out, dir)
		}
	}
	return out
}

// sessionWorkingDir is the current session's own working directory on disk.
func sessionWorkingDir(ctx context.Context) (string, error) {
	w := worldFrom(ctx)
	current, err := w.lastTask()
	if err != nil {
		return "", err
	}
	return filepath.Join(w.storage.Dir, "workspaces", w.workspaceID,
		"projects", w.projectID, "sessions", current.sessionID, "workspace"), nil
}

func sessionMemory(ctx context.Context) (string, error) {
	dir, err := sessionWorkingDir(ctx)
	if err != nil {
		return "", err
	}
	body, found := sandbox.ReadMemory(dir)
	if !found {
		return "", fmt.Errorf("the session has no memory file at %s", dir)
	}
	return body, nil
}

func initializeContextSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, contextKey{}, &contextWorld{}), nil
	})

	// The manual is what a session is told when it is asked to drive the system. Loading it is the
	// ordinary context path, so this says the document is loadable and carries what a session needs,
	// not that setting a context works.
	sc.Step(`^the operator loads the manual as the project's context$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.SetContext(ctx, &quaycrewv1.SetContextRequest{
			Scope: "project", Owner: w.projectID, Body: manual.Text(),
		})
		return w.lastErr
	})

	sc.Step(`^the project's context names the words a system is made of$`, func(ctx context.Context) error {
		projects := contextFrom(ctx).scoped("project")
		if len(projects) != 1 {
			return fmt.Errorf("%d project directories, want 1", len(projects))
		}
		body := projects[0].GetBody()
		for _, word := range []string{"workspace", "project", "session", "session", "sandbox"} {
			if !strings.Contains(body, word) {
				return fmt.Errorf("the context never says %q, so a session would be guessing", word)
			}
		}
		return nil
	})

	sc.Step(`^the project's context says how to set a context$`, func(ctx context.Context) error {
		projects := contextFrom(ctx).scoped("project")
		if len(projects) != 1 {
			return fmt.Errorf("%d project directories, want 1", len(projects))
		}
		if !strings.Contains(projects[0].GetBody(), "krewe context set") {
			return fmt.Errorf("the context never says how anything gets told anything")
		}
		return nil
	})

	sc.Step(`^the operator asks where context lives$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), contextFrom(ctx)
		resp, err := w.client.ListContexts(ctx, &quaycrewv1.ListContextsRequest{})
		if err != nil {
			return err
		}
		c.dirs = resp.GetDirs()
		return nil
	})

	// Every level the system has, so nobody has to know which ones exist to find them.
	sc.Step(`^it names the system, a workspace and a project$`, func(ctx context.Context) error {
		c := contextFrom(ctx)
		for _, scope := range []string{"system", "workspace", "project"} {
			if len(c.scoped(scope)) == 0 {
				return fmt.Errorf("the listing has no %s level in it", scope)
			}
		}
		if got := len(c.scoped("system")); got != 1 {
			return fmt.Errorf("%d system levels, want exactly 1: there is one system", got)
		}
		return nil
	})

	sc.Step(`^it names a workspace directory and a project directory$`, func(ctx context.Context) error {
		c := contextFrom(ctx)
		if got := len(c.scoped("workspace")); got != 1 {
			return fmt.Errorf("%d workspace directories, want 1", got)
		}
		if got := len(c.scoped("project")); got != 1 {
			return fmt.Errorf("%d project directories, want 1", got)
		}
		for _, dir := range c.dirs {
			if dir.GetName() == "" {
				return fmt.Errorf("a %s level has no name, so a listing of them says nothing", dir.GetScope())
			}
			// The system's context belongs to no directory: it is rendered into every workspace's file,
			// so there is no one file to name and nothing to say here.
			if dir.GetScope() != "system" && dir.GetHost() == "" {
				return fmt.Errorf("the %s directory does not say where it is on the host", dir.GetScope())
			}
		}
		return nil
	})

	sc.Step(`^each one says where it appears inside a sandbox$`, func(ctx context.Context) error {
		for _, dir := range contextFrom(ctx).dirs {
			if dir.GetScope() == "system" {
				continue
			}
			want := sandbox.WorkingPath
			if dir.GetScope() == "workspace" {
				want = sandbox.ConversationPath
			}
			if dir.GetSandbox() != want {
				return fmt.Errorf("the %s directory appears at %q inside a sandbox, want %q",
					dir.GetScope(), dir.GetSandbox(), want)
			}
		}
		return nil
	})

	sc.Step(`^each one names the memory file the model reads$`, func(ctx context.Context) error {
		for _, dir := range contextFrom(ctx).dirs {
			if dir.GetScope() == "system" {
				continue
			}
			// It has to sit inside the directory it belongs to, or editing it edits nothing the model
			// will ever read.
			if !strings.HasPrefix(dir.GetMemory(), dir.GetHost()+"/") {
				return fmt.Errorf("the memory file %q is not inside %q", dir.GetMemory(), dir.GetHost())
			}
			if filepath.Base(dir.GetMemory()) != sandbox.MemoryFile {
				return fmt.Errorf("the memory file is %q, want %s", dir.GetMemory(), sandbox.MemoryFile)
			}
		}
		return nil
	})

	sc.Step(`^no context has been written yet$`, func(ctx context.Context) error {
		for _, dir := range contextFrom(ctx).dirs {
			if dir.GetWritten() {
				return fmt.Errorf("the %s directory reports a memory file that was never written", dir.GetScope())
			}
		}
		return nil
	})

	sc.Step(`^the operator sets the project's context to "([^"]*)"$`, func(ctx context.Context, body string) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.SetContext(ctx, &quaycrewv1.SetContextRequest{
			Scope: "project", Owner: w.projectID, Body: body,
		})
		return w.lastErr
	})

	sc.Step(`^the operator sets the session's context to "([^"]*)"$`, func(ctx context.Context, body string) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		_, w.lastErr = w.client.SetContext(ctx, &quaycrewv1.SetContextRequest{
			Scope: string(store.ContextSession), Owner: current.sessionID, Body: body,
		})
		return w.lastErr
	})

	// A CLAUDE.md that was there before the system ever wrote one: no marks in it, because nothing
	// composed it. An operator who dropped a file into the working directory leaves exactly this.
	sc.Step(`^the sandbox's memory file is replaced with "([^"]*)" and no marks$`,
		func(ctx context.Context, body string) error {
			dir, err := sessionWorkingDir(ctx)
			if err != nil {
				return err
			}
			return sandbox.WriteMemory(dir, body)
		})

	sc.Step(`^the operator sets context at scope "([^"]*)" to "([^"]*)"$`,
		func(ctx context.Context, scope, body string) error {
			w := worldFrom(ctx)
			// The system's context is the one level with nothing to own it: it is true everywhere.
			owner := w.projectID
			switch store.ContextScope(scope) {
			case store.ContextSystem:
				owner = ""
			case store.ContextWorkspace:
				owner = w.workspaceID
			}
			_, w.lastErr = w.client.SetContext(ctx, &quaycrewv1.SetContextRequest{
				Scope: scope, Owner: owner, Body: body,
			})
			return nil
		})

	// A session's own working directory, which is where the project's and the session's context land.
	sc.Step(`^the session's memory file carries "([^"]*)"$`, func(ctx context.Context, want string) error {
		body, err := sessionMemory(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(body, want) {
			return fmt.Errorf("the session's memory file reads %q, want it to carry %q", body, want)
		}
		return nil
	})

	// The conversation store's directory, shared by every session in the workspace, which is where the
	// system's and the workspace's context land.
	sc.Step(`^the workspace's memory file carries "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		dir := filepath.Join(w.storage.Dir, "workspaces", w.workspaceID, "claude")
		body, err := os.ReadFile(filepath.Join(dir, sandbox.MemoryFile))
		if err != nil {
			return fmt.Errorf("the workspace's memory file was not written: %w", err)
		}
		if !strings.Contains(string(body), want) {
			return fmt.Errorf("the workspace's memory file reads %q, want it to carry %q", body, want)
		}
		return nil
	})

	// Appending to the file is what an agent writing a note actually does: the directory is mounted in,
	// so this process and that container are looking at one place.
	sc.Step(`^something inside the sandbox writes "([^"]*)" into its memory$`,
		func(ctx context.Context, note string) error {
			dir, err := sessionWorkingDir(ctx)
			if err != nil {
				return err
			}
			existing, _ := sandbox.ReadMemory(dir)
			return sandbox.WriteMemory(dir, strings.TrimRight(existing, "\n")+"\n"+note)
		})

	sc.Step(`^the project's context reads "([^"]*)"$`, func(ctx context.Context, want string) error {
		projects := contextFrom(ctx).scoped("project")
		if len(projects) != 1 {
			return fmt.Errorf("%d project directories, want 1", len(projects))
		}
		if got := projects[0].GetBody(); got != want {
			return fmt.Errorf("the project's context reads %q, want %q", got, want)
		}
		return nil
	})

	// Asked of the store rather than of a listing, because a session's context belongs to one
	// conversation and the listing is about a project.
	sc.Step(`^the session's context reads "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		got, err := w.store.GetContext(ctx, store.ContextSession, current.sessionID)
		if err != nil {
			return err
		}
		if !strings.Contains(got, want) {
			return fmt.Errorf("the session's context reads %q, want it to carry %q", got, want)
		}
		return nil
	})

	sc.Step(`^the project's context has been written$`, func(ctx context.Context) error {
		projects := contextFrom(ctx).scoped("project")
		if len(projects) != 1 {
			return fmt.Errorf("%d project directories, want 1", len(projects))
		}
		if !projects[0].GetWritten() {
			return fmt.Errorf("the project's memory file was written and the listing does not say so")
		}
		return nil
	})

	sc.Step(`^it names (\d+) workspace directory and (\d+) project directories$`,
		func(ctx context.Context, workspaces, projects int) error {
			c := contextFrom(ctx)
			if got := len(c.scoped("workspace")); got != workspaces {
				return fmt.Errorf("%d workspace directories, want %d", got, workspaces)
			}
			if got := len(c.scoped("project")); got != projects {
				return fmt.Errorf("%d project directories, want %d", got, projects)
			}
			return nil
		})

	// The steps that read a level back through the real command line tool.
	//
	// A level could be written and never read back, so it could only be overwritten: adding a
	// paragraph meant already holding the whole text. These run the tool in its own process, because
	// what is specified here is what a redirection captures. Standard output has to be the body and
	// nothing else, and a level that says nothing has to be told apart from a read that broke, which
	// is the exit status and exists nowhere inside the test process.

	sc.Step(`^the caller reads the project's context back$`, func(ctx context.Context) error {
		return runTool(ctx, "context", "show", whereTheProjectIs(ctx))
	})

	sc.Step(`^the caller reads the system's context back$`, func(ctx context.Context) error {
		return runTool(ctx, "context", "show", string(store.ContextSystem))
	})

	// What came out of the last read goes straight back in, which is the pair the command exists for.
	sc.Step(`^the caller writes back what it read$`, func(ctx context.Context) error {
		return runToolSaying(ctx, toolFrom(ctx).stdout, "context", "set", whereTheProjectIs(ctx))
	})

	sc.Step(`^the caller writes back what it read with "([^"]*)" added$`,
		func(ctx context.Context, added string) error {
			held := toolFrom(ctx).stdout
			if strings.TrimSpace(held) == "" {
				return fmt.Errorf("the read gave nothing back, so there is nothing to add to")
			}
			return runToolSaying(ctx, held+"\n"+added+"\n", "context", "set", whereTheProjectIs(ctx))
		})
}

// The steps that write and read context through the real command line tool.
//
// How big a level is only matters where a person sees it, and the person sees it on standard output.
// So these run the tool in its own process rather than calling the control plane, which is the only
// place the size is rendered at all.
func initializeContextSizeSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator has (\d+) characters to say$`, func(ctx context.Context, length int) error {
		toolFrom(ctx).stdin = strings.Repeat("a", length)
		return nil
	})

	sc.Step(`^the operator sets the (system|workspace|project)'s context with the tool$`,
		func(ctx context.Context, scope string) error {
			w := worldFrom(ctx)
			target := "system"
			switch scope {
			case "workspace":
				target = w.workspaceName
			case "project":
				target = w.workspaceName + "/" + w.projectName
			}
			return runToolSaying(ctx, toolFrom(ctx).stdin, "context", "set", target)
		})

	sc.Step(`^the operator lists the context levels with the tool$`, func(ctx context.Context) error {
		return runTool(ctx, "context")
	})
}

package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
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

func initializeContextSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, contextKey{}, &contextWorld{}), nil
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
				return fmt.Errorf("a %s directory has no name, so a listing of them says nothing", dir.GetScope())
			}
			if dir.GetHost() == "" {
				return fmt.Errorf("the %s directory does not say where it is on the host", dir.GetScope())
			}
		}
		return nil
	})

	sc.Step(`^each one says where it appears inside a sandbox$`, func(ctx context.Context) error {
		for _, dir := range contextFrom(ctx).dirs {
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

	// Writing the file directly is what the operator does with an editor. An empty directory is the
	// normal state and says nothing, so this is the difference the listing has to show.
	sc.Step(`^the memory file for the project is written$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		dir := filepath.Join(w.storage.Dir, "workspaces", w.workspaceID, "projects", w.projectID, "workspace")
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, sandbox.MemoryFile), []byte("# context\n"), 0o666)
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
}

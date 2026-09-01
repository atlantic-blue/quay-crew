package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/hook"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The system's settings say two things now: which hooks a session runs under, and the line the runtime
// draws under the conversation. Every session gets them, because an operator attached to a session
// with no hooks needs that line as much as anybody.
//
// The file has to be there before a task is told to load it. The runtime refuses to start on a
// settings file that is missing, saying only "Settings file not found", and that would be every task
// on the system rather than one.
func TestATaskLoadsTheSystemsSettingsOnlyWhenTheyAreOnDisk(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(Config{
		Store: store.NewMemory(), Runner: model.EchoRunner{},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Storage: sandbox.Storage{Dir: dir, Host: dir},
	})
	ctx := context.Background()
	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	session := &quaycrewv1.Session{Workspace: workspace.GetWorkspace().GetId()}

	if _, mounted := server.renderHooks(ctx, session); !mounted {
		t.Fatal("a session under no hooks was given no settings directory, so it has no status line")
	}
	want := sandbox.HooksPath + "/" + hook.SettingsFile
	if got := server.settingsFor(ctx, session); got != want {
		t.Errorf("the task was told to load %q, want %q", got, want)
	}

	// A system whose data directory has been taken away underneath it. Naming the file anyway is a
	// task that dies before the model sees it.
	at, _ := server.storage.WorkspaceHooksDir(session.GetWorkspace())
	if err := os.Remove(filepath.Join(at, hook.SettingsFile)); err != nil {
		t.Fatalf("take the settings away: %v", err)
	}
	if got := server.settingsFor(ctx, session); got != "" {
		t.Errorf("the task was told to load %q, and there is no such file", got)
	}
}

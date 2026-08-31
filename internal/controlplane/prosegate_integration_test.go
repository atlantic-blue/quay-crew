//go:build integration

package controlplane_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
)

// The whole path, with nothing standing in for anything: an operator imports the shipped prose gate
// through the control plane, attaches it to a workspace, dispatches a task, and the gate refuses a
// long sentence inside the container the daemon actually made.
//
// Each half is proved elsewhere and neither says the two meet. The hook's own module says what it
// decides, from Go values. The scenarios in features/ run the built entry point on this machine. What
// neither can say is that the file the control plane wrote into a sandbox is an executable that
// image can run, bound to the events the runtime raises. The entry point is built rather than
// committed, so a binary built for the wrong processor loads here and nowhere else, and the failure
// is a hook that never refuses anything. That reads exactly like a hook that approves.
//
// QC_TEST_SANDBOX_IMAGE names the image, and without one there is nothing to prove against, so this
// says so rather than passing.
func TestTheProseGateRefusesInsideTheContainerTheControlPlaneMade(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the system's sandbox image to run this")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	runner, err := model.NewRunner("echo", "", "")
	if err != nil {
		t.Fatalf("model runner: %v", err)
	}
	// A hook reaches a container as a mount, so the system needs somewhere on the host to write one.
	// Without a data directory it writes nothing, mounts nothing and refuses nothing, and the task
	// still runs, which is the trade renderHooks makes deliberately. This test runs beside the daemon
	// rather than inside a container, so the path this process writes and the path the daemon mounts
	// are the same one.
	data := t.TempDir()
	server := controlplane.NewServer(controlplane.Config{
		Store:    store.NewMemory(),
		Runner:   runner,
		Provider: sandbox.DockerProvider{Image: image},
		Secrets:  secrets.NewMemory(),
		Storage:  sandbox.Storage{Dir: data, Host: data},
	})

	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Imported through the control plane out of the files this build ships, which is what the command
	// line tool does when an operator types `krewe hook import`.
	gate := shippedProseGate(t)
	files := make([]*quaycrewv1.HookFile, 0, len(gate.Files))
	for _, one := range gate.Files {
		files = append(files, &quaycrewv1.HookFile{
			Path: one.Path, Body: one.Body, Executable: one.Executable,
		})
	}
	if _, err := server.ImportHook(ctx, &quaycrewv1.ImportHookRequest{Files: files}); err != nil {
		t.Fatalf("import the prose gate: %v", err)
	}

	// Attached, not seeded. This gate refuses prose, and prose is what a role produces all day, so a
	// workspace opts in. A test that skipped this step would prove the mount and not the opt in.
	if _, err := server.AttachHook(ctx, &quaycrewv1.AttachHookRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: gate.Name,
	}); err != nil {
		t.Fatalf("attach the prose gate: %v", err)
	}

	dispatched, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "hello",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	container := sandbox.ContainerName(dispatched.GetId())
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })

	// The runtime finds the hook through the settings the system rendered, so that is where the path
	// comes from here too. A test that built the path itself would pass over settings that name a
	// file which is not there.
	settings, err := exec.CommandContext(ctx, "docker", "exec", container,
		"cat", sandbox.HooksPath+"/"+hook.SettingsFile).Output()
	if err != nil {
		t.Fatalf("the container carries no rendered settings: %v", err)
	}
	for _, needed := range []string{gate.Name, "Write|Edit|MultiEdit", "Bash"} {
		if !strings.Contains(string(settings), needed) {
			t.Fatalf("the settings do not bind %q, so the runtime never calls the gate:\n%s",
				needed, settings)
		}
	}
	command := sandbox.HooksPath + "/" + gate.Name + "/" + gate.Events[0].Entry

	for _, one := range []struct {
		name    string
		payload string
		refused bool
	}{
		{
			// Refusing comes first. A gate that always passes satisfies every test about passing.
			name: "prose the standard refuses",
			payload: `{"tool_name":"Write","tool_input":{"file_path":"docs/HOOKS.md",` +
				`"content":"The control plane reads the row and answers the question the caller asked, ` +
				`and it does that before the session starts, because nothing else reads that row at all."}}`,
			refused: true,
		},
		{
			// The half that decides whether the gate is worth attaching. Every role in this system
			// writes prose on every slice, so a wrong refusal here stops the system delivering.
			name: "prose the standard allows",
			payload: `{"tool_name":"Write","tool_input":{"file_path":"docs/HOOKS.md",` +
				`"content":"The gate reads the prose. It refuses a long sentence. It says what to do."}}`,
			refused: false,
		},
		{
			// A Go file is not prose. A gate that measured sentence length in source would refuse
			// every file in this repository.
			name: "the same words in a source file",
			payload: `{"tool_name":"Write","tool_input":{"file_path":"internal/hook/hook.go",` +
				`"content":"The control plane reads the row and answers the question the caller asked, ` +
				`and it does that before the session starts, because nothing else reads that row at all."}}`,
			refused: false,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			run := exec.CommandContext(ctx, "docker", "exec", "-i", container, command)
			run.Stdin = strings.NewReader(one.payload)
			var said strings.Builder
			run.Stderr = &said
			err := run.Run()

			// A binary that cannot run in this image answers with the exit code of a broken exec
			// rather than with a decision, and this is the failure this test exists for.
			for _, broken := range []string{
				"exec format error", "cannot execute binary file", "Permission denied", "not found",
			} {
				if strings.Contains(said.String(), broken) {
					t.Fatalf("the gate did not run inside the image: %s", said.String())
				}
			}
			refused := exitCode(err) == 2
			if refused != one.refused {
				t.Fatalf("the gate refused: %t, want %t\n%s", refused, one.refused, said.String())
			}
			if refused && !strings.Contains(said.String(), "Simplified Technical English") {
				t.Errorf("the refusal does not name the standard, so the writer is left guessing:\n%s",
					said.String())
			}
		})
	}
}

// shippedProseGate is the gate as this build ships it, or a failure naming what is missing.
func shippedProseGate(t *testing.T) hook.Hook {
	t.Helper()
	hooks, err := hook.Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v (run `make hooks` first: the entry point is built)", err)
	}
	for _, one := range hooks {
		if one.Name == "prose-gate" {
			return one
		}
	}
	t.Fatal("this build ships no prose gate, so this test proves nothing")
	return hook.Hook{}
}

// exitCode is how a hook answers: 2 is a refusal, 0 is anything else.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ended *exec.ExitError
	if errors.As(err, &ended) {
		return ended.ExitCode()
	}
	return -1
}

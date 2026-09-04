//go:build integration

package controlplane_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/hook"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The whole path, with nothing standing in for anything: a fresh system seeds the process gate,
// dispatches an exec, and the gate refuses a command inside the container the daemon actually made.
//
// Each half is proved elsewhere and neither says the two meet. The hook's own module says what it
// decides, from Go values. The scenarios in features/ run the built entry point on this machine.
// What neither can say is that the file the control plane wrote into a sandbox is an executable that
// image can run, bound to the events the runtime raises. The entry point is built rather than
// committed, so a binary built for the wrong processor loads here and nowhere else, and the failure
// is a hook that never refuses anything. That reads exactly like a hook that approves.
//
// QC_TEST_SANDBOX_IMAGE names the image, and without one there is nothing to prove against, so this
// says so rather than passing.
func TestTheProcessGateRefusesInsideTheContainerTheControlPlaneMade(t *testing.T) {
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
	// Seeded rather than attached, which is the half a test of the matcher cannot reach. Nobody
	// imports anything and nobody attaches anything, because that is every system on its first day.
	server.SeedHooks(ctx, "../../hooks", slog.New(slog.DiscardHandler))

	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
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
	for _, needed := range []string{"process-gate", "Bash", "PreToolUse"} {
		if !strings.Contains(string(settings), needed) {
			t.Fatalf("the settings do not bind %q, so the runtime never calls the gate:\n%s",
				needed, settings)
		}
	}
	command := sandbox.HooksPath + "/process-gate/bin/hook"

	for _, one := range []struct {
		name    string
		command string
		refused bool
	}{
		// Refusing comes first. A gate that always passes satisfies every test about passing.
		{name: "the terminal the operator works in", command: "tmux kill-server", refused: true},
		{name: "the containers the system is", command: "docker compose down", refused: true},
		{name: "a signal to a process it did not start", command: "kill -9 4213", refused: true},
		{name: "the session lifting its own gate", command: "KREWE_MAY_END_A_PROCESS=1 kill -9 4213", refused: true},
		// The half that decides whether the gate is worth seeding. Ending a job ends the work in the
		// record and signals nothing, so the product's own way through has to stay open.
		{name: "ending a job", command: "krewe job stop 31a6d96d", refused: false},
		{name: "the work a session does all day", command: "go test -count=1 ./...", refused: false},
	} {
		t.Run(one.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"tool_name":  "Bash",
				"tool_input": map[string]string{"command": one.command},
			})
			if err != nil {
				t.Fatalf("payload: %v", err)
			}
			run := exec.CommandContext(ctx, "docker", "exec", "-i", container, command)
			run.Stdin = strings.NewReader(string(payload))
			var said strings.Builder
			run.Stderr = &said
			err = run.Run()

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
			if refused && !strings.Contains(said.String(), "krewe job stop") {
				t.Errorf("the refusal does not name the way through, so the session tries the next spelling:\n%s",
					said.String())
			}
		})
	}
}
